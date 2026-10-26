r"""E2E tạm: chạy bộ testcase trong docs_test_case_and_data/testcase bằng data thật
trong docs_test_case_and_data/'Tài Liệu Mẫu' qua API /chat.

Luồng:
  login -> tạo category mới -> upload + tạo bài (doc_type=laws) các PDF quy định
  -> POLL /sections đến khi index done -> parse test_case.md (Câu hỏi/Đáp án)
  -> POST /chat (scope category_ids để không lẫn bài test khác của user)
  -> tự chấm: mọi token số của đáp án mong đợi (30.000 / 300% / 07 ngày ...)
     phải xuất hiện trong answer (chuẩn hoá ngăn cách nghìn & số 0 đầu);
     case không có số -> MANUAL (in 2 câu để người lớn mắt chấm).

Chạy:
  python e2e_testcases.py                       # 2 PDF quy định + test_case.md
  python e2e_testcases.py --max 5               # chạy 5 case đầu cho nhanh
  python e2e_testcases.py --file ../docs_test_case_and_data/testcase/test_case_philosophy.md \
      --docs "D:\code\projects\taskforge\knowledge_ingestion\docs_test_case_and_data\Tài Liệu Mẫu\triethoc_tay_phuong.pdf"
"""

import argparse
import json
import re
import sys
import time
import unicodedata
import uuid
from pathlib import Path

import requests

from seed import auth, create_category, login, presign_and_upload

HERE = Path(__file__).resolve().parent
DATA = HERE.parent / "docs_test_case_and_data"
TAI_LIEU = DATA / "Tài Liệu Mẫu"

DEFAULT_DOCS = [TAI_LIEU / "quy_dinh_noi_bo.pdf", TAI_LIEU / "quy_dinh_bo_sung.pdf"]
DEFAULT_CASES = DATA / "testcase" / "test_case.md"

# Provider dự phòng khi provider đang gọi lỗi kiểu đổi backend được
# (9router hết credit -> 402, bị chặn -> 401/403, quá tải -> 408/429,
# sự cố -> 5xx). Đặt [] để tắt xoay.
FALLBACK_PROVIDERS = ["ollama", "openrouter"]
# HTTP status coi là lỗi phía provider — được phép xoay provider.
ROTATE_STATUS = {401, 402, 403, 408, 429}


def _rotate_on(status: int) -> bool:
    return status in ROTATE_STATUS or status >= 500

fails: list[str] = []


def norm_vi(s: str) -> str:
    """bỏ dấu + lowercase + siết khoảng trắng — so chuỗi tiếng Việt lỏng."""
    s = unicodedata.normalize("NFD", s)
    s = "".join(c for c in s if unicodedata.category(c) != "Mn")
    return re.sub(r"\s+", " ", s.lower().strip())


def num_variants(raw: str) -> set[str]:
    """Every spelling of one number token that a correct answer may legitimately use.

    A real batch run exposed two false negatives: the expected text says
    "22,5" (Vietnamese decimal comma) while the model writes "22.5", and
    "29.970.000" may come back as "29970000" or "29,970,000". Comparing the
    variant SET instead of the raw token keeps grading format-agnostic."""
    digits = re.sub(r"[^\d]", "", raw)
    variants = {raw, digits}
    if len(digits) > 3:
        variants.add(f"{digits[:-3]}.{digits[-3:]}")
        variants.add(f"{digits[:-3]},{digits[-3:]}")
    dec = re.fullmatch(r"(\d+)([.,])(\d{1,3})", raw)
    if dec:
        other = "," if dec.group(2) == "." else "."
        variants.add(dec.group(1) + other + dec.group(3))
    return variants


def num_requirements(expected: str) -> list[frozenset[str]]:
    """Mỗi số trong đáp án mong đợi -> 1 yêu cầu là TẬP biến thể chấp nhận được:
    30.000 -> {30.000, 30000}; 07 -> {07, 7}. Đủ khi answer chứa ÍT NHẤT một
    biến thể của từng yêu cầu (không phải mọi biến thể)."""
    reqs, seen = [], set()
    for m in re.finditer(r"\d[\d.,]*", expected):
        raw = m.group().rstrip(".,")
        digits = re.sub(r"[^\d]", "", raw)
        variants = num_variants(raw)
        if len(digits) > 3:  # kiểu nghìn 30000 <-> 30.000
            variants |= num_variants(digits)
        key = digits
        if key not in seen:
            seen.add(key)
            reqs.append(frozenset(variants))
    return reqs


def parse_cases(path: Path) -> list[dict]:
    """Case block: ### Case N / **Câu hỏi:** / **Đáp án mong đợi:** (n hiều dòng)
    / **Nguồn tham khảo:**."""
    text = path.read_text(encoding="utf-8")
    cases = []
    for block in re.split(r"^### ", text, flags=re.M)[1:]:
        title = block.splitlines()[0].strip()
        q = re.search(r"\*\*Câu hỏi:\*\*\s*(.+)", block)
        a = re.search(r"\*\*Đáp án mong đợi:\*\*\s*(.+?)(?=\*\*Nguồn tham khảo|\Z)",
                      block, flags=re.S)
        src = re.search(r"\*\*Nguồn tham khảo:\*\*\s*(.+)", block)
        if q and a:
            cases.append({"id": title, "question": q.group(1).strip(),
                          "expected": a.group(1).strip(),
                          "source": src.group(1).strip() if src else ""})
    return cases


def get(base, token, path):
    r = requests.get(f"{base}{path}", headers=auth(token), timeout=60)
    return r.status_code, r.json()


def wait_index_done(base, token, article_id, timeout_s, interval_s):
    t0 = time.time()
    while time.time() - t0 < timeout_s:
        _, body = get(base, token, f"/articles/{article_id}/sections")
        data = body.get("data") or {}
        if data.get("index_status") == "done":
            return data
        print(f"  ... bài {article_id}: index={data.get('index_status')} ({time.time()-t0:.0f}s)")
        time.sleep(interval_s)
    raise SystemExit(f"hết {timeout_s}s mà bài {article_id} chưa index xong")


def upload_doc(base, token, path: Path, category_id, name, laws=True):
    key = presign_and_upload(base, token, path, "application/pdf")
    payload = {"name": name, "content_type": "application/pdf", "category_id": category_id,
               "storage_key": key}
    if laws:
        payload["doc_type"] = "laws"
    r = requests.post(f"{base}/articles", json=payload, headers=auth(token), timeout=60)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"tạo bài {path.name}: {body}")
    return body["data"]["id"]


def ask_chat(base, token, question, category_ids, timeout_s, provider="", fallbacks=None):
    """POST /chat, tự xoay provider khi backend lỗi phía provider.

    Thứ tự: provider của case -> fallbacks (mặc định FALLBACK_PROVIDERS).
    Chỉ xoay khi lỗi thuộc loại đổi backend là đáng (401/402/403 hết
    quyền/credit, 408/429 quá tải, 5xx sự cố, lỗi mạng); lỗi phía request
    (400/404/422 — model/tham số sai) trả luôn vì đổi provider cũng hỏng.

    Trả (data | None, lỗi, số giây); data có thêm "_provider" = provider đã
    trả lời (rỗng = default cấu hình BE) để report biết đã xoay hay chưa.
    """
    chain = [provider]
    for fb in (FALLBACK_PROVIDERS if fallbacks is None else fallbacks):
        if fb and fb not in chain:
            chain.append(fb)
    t0 = time.time()
    err = ""
    for i, p in enumerate(chain):
        payload = {"message": question, "category_ids": category_ids}
        if p:
            payload["provider"] = p
        try:
            r = requests.post(f"{base}/chat", json=payload, headers=auth(token), timeout=timeout_s)
        except requests.exceptions.RequestException as e:
            err = f"client {type(e).__name__} sau {time.time()-t0:.0f}s: {str(e)[:120]}"
        else:
            try:
                body = r.json()
            except ValueError:
                body = None
            if body and body.get("status"):
                data = body["data"]
                data["_provider"] = p
                return data, "", time.time() - t0
            err = f"HTTP {r.status_code}: {str(body or r.text)[:200]}"
            if not _rotate_on(r.status_code):
                return None, err, time.time() - t0
        nxt = chain[i + 1] if i + 1 < len(chain) else None
        if nxt is None:
            break
        print(f"    ! {p or 'default'} lỗi -> xoay sang {nxt or 'default'} ({err})")
    return None, err, time.time() - t0


def grade(case, data) -> str:
    answer = (data or {}).get("answer", "")
    reqs = num_requirements(case["expected"])
    if not reqs:
        return "MANUAL"
    na = norm_vi(answer)
    missing = [sorted(r)[0] for r in reqs if not any(v in na for v in r)]
    return "PASS" if not missing else "FAIL:" + ",".join(missing[:4])


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:9002/api/v1")
    ap.add_argument("--username", default="p1a2760b8")
    ap.add_argument("--password", default="secret123")
    ap.add_argument("--file", default=str(DEFAULT_CASES), help="file testcase dạng Case block")
    ap.add_argument("--docs", nargs="*", default=None, help="PDF cần upload (mặc định 2 quy định)")
    ap.add_argument("--skip-upload", action="store_true",
                    help="dùng lại category --category-id, không upload lại")
    ap.add_argument("--category-id", type=int, default=0)
    ap.add_argument("--max", type=int, default=0, help="chạy tối đa N case (0 = hết)")
    ap.add_argument("--chat-timeout", type=int, default=300,
                    help="timeout 1 lượt chat (s) — 9router model lớn có lúc >100s")
    ap.add_argument("--providers", nargs="*", default=[],
                    help="luân phiên provider từng case (vd: 9router openrouter ollama); "
                         "rỗng = default cấu hình BE")
    ap.add_argument("--fallbacks", nargs="*", default=FALLBACK_PROVIDERS,
                    help="provider xoay khi provider của case lỗi 401/402/403/408/429/5xx "
                         f"(mặc định: {' '.join(FALLBACK_PROVIDERS)}); rỗng = tắt xoay")
    ap.add_argument("--wait", type=int, default=600, help="chờ index tối đa (s)")
    args = ap.parse_args()

    token = login(args.base, args.username, args.password)
    print(f"login ok ({args.username}) base={args.base}")

    if args.skip_upload:
        cat = [args.category_id]
        print(f"dùng category có sẵn {cat}")
    else:
        docs = [Path(d) for d in (args.docs or [str(d) for d in DEFAULT_DOCS])]
        cat_id = create_category(args.base, token, f"TC {uuid.uuid4().hex[:6]}")
        created = []
        for d in docs:
            laws = "quy_dinh" in d.name.lower()
            aid = upload_doc(args.base, token, d, cat_id, f"[tc] {d.stem}", laws=laws)
            created.append(aid)
            print(f"đã tạo bài {aid} từ {d.name} (laws={laws})")
        for aid in created:
            wait_index_done(args.base, token, aid, args.wait, 8)
        cat = [cat_id]
        print(f"category scope = {cat_id} (lần sau chạy: --skip-upload --category-id {cat_id})")

    cases = parse_cases(Path(args.file))
    if args.max:
        cases = cases[:args.max]
    print(f"\n{len(cases)} case từ {Path(args.file).name} — gọi /chat từng câu...")

    report = []
    for i, c in enumerate(cases):
        provider = args.providers[i % len(args.providers)] if args.providers else ""
        data, err, secs = ask_chat(args.base, token, c["question"], cat, args.chat_timeout,
                                   provider, args.fallbacks)
        used = (data or {}).get("_provider", "")
        verdict = "ERROR" if err else grade(c, data)
        cites = (data or {}).get("cites") or []
        report.append({"case": c["id"], "provider": provider or "default",
                       "answered_by": used or ("default" if data else ""),
                       "question": c["question"],
                       "expected": c["expected"], "answer": (data or {}).get("answer", ""),
                       "error": err,
                       "sources": (data or {}).get("sources", ""),
                       "n_cites": len(cites), "secs": round(secs, 1), "verdict": verdict})
        tag = provider or "default"
        if used and used != provider:
            tag += f"->{used or 'default'}"
        line = f"[{c['id']:<8}] [{tag}] {verdict:<12} {secs:5.1f}s cites={len(cites)}"
        print(line + (f"  err={err}" if err else ""))
        if verdict.startswith("FAIL") or verdict == "MANUAL" or err:
            print("    Q :", c["question"][:100])
            print("    Expected:", re.sub(r"\s+", " ", c["expected"])[:160])
            print("    Answer  :", re.sub(r"\s+", " ", (data or {}).get("answer", ""))[:220])

    out = HERE / "testcase_results.json"
    out.write_text(json.dumps(report, ensure_ascii=False, indent=1), encoding="utf-8")
    n = {"PASS": 0, "FAIL": 0, "MANUAL": 0, "ERROR": 0}
    for r in report:
        n["FAIL" if r["verdict"].startswith("FAIL") else r["verdict"]] += 1
    print(f"\nTỔNG: {len(report)} case -> PASS={n['PASS']} FAIL={n['FAIL']} "
          f"MANUAL={n['MANUAL']} ERROR={n['ERROR']} | report: {out.name}")
    sys.exit(1 if n["FAIL"] or n["ERROR"] else 0)


if __name__ == "__main__":
    main()
