r"""Đánh giá CHẤT LƯỢNG câu trả lời theo lô, xoay nhiều provider LLM thật.

Khác `e2e_testcases.py` (đo "backend có chạy không" + so số trong đáp án), ở
đây câu hỏi chính là "trả lời có như chuyên gia không", nên có ba lớp chấm
(§4 của plan Đợt 25) và mỗi câu đi qua MỘT PHIÊN chat lưu DB — tức vừa test
chất lượng, vừa test đường S1/S4 của Đợt 24 bằng LLM thật.

Vì SAO batch-size mặc định = 4 (đừng "tối ưu" thành 20):
giới hạn 40 request/phút tính theo LƯỢT GỌI LLM, không theo lượt /chat. Một
câu hỏi /chat bên trong là 8-10 lượt gọi LLM (agent tool-loop + classify +
retry; thực đo Đợt 24: một câu sinh 8 dòng `llm: request`, mất 4m21s). 4 câu
trong một cửa sổ 60s ~= 32-40 lượt LLM -> sát trần nhưng không vượt. Muốn
nhanh hơn thì tăng `--concurrency` (chồng nhịp chờ), KHÔNG tăng batch-size.

Không đọc `configs/config.local.json`: mọi lượt gọi LLM đi qua /chat của
chính server, nên không có đường nào để API key rò vào log/report. Report chỉ
ghi TÊN provider/model.

Chạy:
  python expert_eval.py --providers 9router nvidia openrouter --max 6
  python expert_eval.py --file ../docs_test_case_and_data/testcase/context_test.md \
      --providers nvidia            # cặp Q1/Q2 chứng minh server là nguồn lịch sử
  python expert_eval.py --skip-upload --category-id 42 --providers openrouter
"""

import argparse
import json
import re
import sys
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import requests

from e2e_testcases import (DEFAULT_DOCS, auth, create_category, grade, login,
                           norm_vi, num_variants, parse_cases,
                           wait_index_done)
from seed import register

HERE = Path(__file__).resolve().parent
CASES_DIR = HERE.parent / "docs_test_case_and_data" / "testcase"

# Cụm từ cho thấy câu trả lời là "máy tra" chứ không phải người có nghề.
FILLER_RE = re.compile(
    r"(câu hỏi của bạn rất hay|rất hay khi bạn|cảm ơn bạn|xin lỗi bạn|"
    r"tôi là (một )?(AI|trợ lý)|tôi hy vọng|chúc bạn|hãy cùng nhau)", re.I)
# Mở đầu mô tả việc sắp làm thay vì trả lời (phạm mục 10 prompt).
META_OPENER_RE = re.compile(
    r"(^|\n)\s*(tôi sẽ|tôi xin|dựa trên kết quả tìm kiếm|"
    r"kết quả tìm kiếm cho thấy|theo thông tin tôi tìm được)", re.I)
# Lộ nội bộ hệ thống: tên tool, tên collection, điểm similarity.
PLUMBING_RE = re.compile(
    r"(article_chunks|chunk_index|read_article|list_sheets|read_sheet|"
    r"search_documents|compile_tree|similarity|score[:=] ?0?\.\d|"
    r"sheet_count|row_count|page_count|column_count|top_k|point_id|kb_id)", re.I)
MARKER_RE = re.compile(r"\[\^(\d+)\]")
NUM_RE = re.compile(r"\d[\d.,]*")
SELF_SUMMARY_RE = re.compile(r"(tóm lại|nói chung là|như (vậy|trên)[,.])", re.I)


# ------------------------------------------------------------- case loaders

def parse_pairs(path: Path) -> list[dict]:
    """Block `### Pair X` trong context_test.md: Q1/Đáp án Q1/Q2/Đáp án Q2/
    Dấu hiệu mất context. Q2 chứa tham chiếu ("con số vừa trả lời") nên chỉ
    đúng khi có lịch sử — chạy hai lượt, lượt sau KHÔNG gửi history."""
    text = path.read_text(encoding="utf-8")
    pairs = []
    for block in re.split(r"^### ", text, flags=re.M)[1:]:
        title = block.splitlines()[0].strip()

        def field(name, flags=0):
            m = re.search(rf"\*\*{re.escape(name)}:\*\*\s*(.+?)(?=\n- \*\*|\Z)",
                          block, flags | re.S)
            return m.group(1).strip() if m else ""

        q1, q2 = field("Q1"), field("Q2")
        if not q1 or not q2:
            continue
        pairs.append({"id": title, "kind": "pair", "q1": q1, "a1": field("Đáp án Q1"),
                      "q2": q2, "a2": field("Đáp án Q2 (khi có context)"),
                      "loss": field("Dấu hiệu mất context")})
    return pairs


def load_tasks(path: Path, max_n: int) -> list[dict]:
    """Một task = một lần hỏi (case) hoặc một cặp hỏi liên tiếp (pair)."""
    if "context_test" in path.name:
        tasks = parse_pairs(path)
    else:
        tasks = [dict(c, kind="single") for c in parse_cases(path)]
    return tasks[:max_n] if max_n else tasks


# ----------------------------------------------------------- L0/L1/L2 rubric

def markers(answer: str) -> set[int]:
    return {int(n) for n in MARKER_RE.findall(answer)}


def cite_numbers(cites: list[dict]) -> set[int]:
    return {int(c.get("n", 0)) for c in cites if isinstance(c, dict)}


def check_layer0(data: dict, want_provider: str) -> list[str]:
    """Hợp lệ kỹ thuật — sai là loại cả câu, không cộng trừ điểm hình thức."""
    errs = []
    answer = (data or {}).get("answer", "")
    cites = (data or {}).get("cites") or []
    if not answer.strip():
        return ["EMPTY_ANSWER"]
    missing = markers(answer) - cite_numbers(cites)
    if missing:
        errs.append(f"DANGLING_MARKER:{sorted(missing)[:4]}")
    if not cites:
        errs.append("NO_CITES")
    got = (data or {}).get("_provider", "")
    if want_provider and got != want_provider:
        errs.append(f"PROVIDER_DRIFT:{got or 'default'}")
    qt = (data or {}).get("question_type", "")
    if not qt:
        errs.append("NO_QUESTION_TYPE")
    return errs


def check_layer1(task: dict, data: dict, which: str) -> dict:
    """Đúng dữ kiện: so token số trong đáp án mong đợi (giữ nguyên grade()) +
    số trong answer phải truy được ra snippet của một cite.

    `unsourced` là CHỈ BÁO chứ không phải lỗi: tổng/tích tính từ số có nguồn
    cũng rơi vào đây (vd 130.350.000 = 48.750.000 + 81.600.000), nên report in
    danh sách để người đọc xác nhận phép tính — đó là phần "còn lại" của
    Ragas faithfulness làm được mà không cần LLM chấm."""
    expected = task.get(which, "")
    answer = (data or {}).get("answer", "")
    verdict = grade({"expected": expected}, data)
    snippets = " ".join(str(c.get("snippet", ""))
                        for c in ((data or {}).get("cites") or []))
    # `hay` KHÔNG được chứa answer: chứa answer thì mọi số trong answer đều
    # "có nguồn" một cách giả tạo và phép thử mất nghĩa.
    hay = norm_vi(f"{snippets} {expected}")
    hay_nums = {v for tok in NUM_RE.findall(hay) for v in num_variants(tok)}
    unsourced = []
    for raw in NUM_RE.findall(answer):
        digits = re.sub(r"[^\d]", "", raw)
        if len(digits) < 3:
            continue  # số nhỏ (điều/khoản/trang) không đủ để kết luận bịa
        if len(digits) == 4 and 1900 <= int(digits) <= 2100:
            continue  # năm: ngày tháng trong tài liệu dày đặc, soi không bổ ích
        variants = num_variants(raw)
        # So theo tập biến thể chứ không theo chuỗi con: answer viết "22.5"
        # trong khi snippet ghi "22,5" là cùng một số, không phải số bịa.
        if not (variants & hay_nums) and not any(v in hay for v in variants):
            unsourced.append(raw)
    return {"factual": verdict, "unsourced": sorted(set(unsourced))[:8]}


def check_layer2(task: dict, data: dict) -> dict:
    """Hình thức chuyên gia bằng heuristics. Chấm trên TỪNG lượt, in đủ tên
    tiêu chí trượt để người đọc biết mất điểm vì câu nào.

    Bốn tiêu chí còn lại của rubric (giải thích thuật ngữ, nêu điều kiện
    trước phán quyết, chốt bằng bước tiếp theo, độ dài tương xứng) heuristic
    không kết luận được -> để người chấm, ghi trong expert_answer_rubric.md."""
    answer = (data or {}).get("answer", "")
    first = answer.split(".")[0]
    checks = {
        "verdict_first": bool(markers(first) or NUM_RE.search(first)),
        "no_meta_opener": not META_OPENER_RE.search(answer[:200]),
        "no_filler": not FILLER_RE.search(answer),
        "no_plumbing_leak": not PLUMBING_RE.search(answer),
        "no_self_summary": not SELF_SUMMARY_RE.search(answer[-300:]),
        # ~1 chú thích cho mỗi 800 ký tự: thưa hơn nghĩa là thiếu căn cứ.
        "citation_density": bool(markers(answer)) and (
            len(markers(answer)) * 800 >= len(answer)),
    }
    passed = sum(1 for ok in checks.values() if ok)
    return {"shape": f"{passed}/{len(checks)}",
            "failed": [k for k, ok in checks.items() if not ok],
            "chars": len(answer)}


# ---------------------------------------------------------------- transport

class Governor:
    """Cửa sổ trượt 60 giây, tối đa `batch` lượt phát trong một cửa sổ."""

    def __init__(self, batch: int, rpm: int):
        self.batch = max(1, min(batch, rpm))
        self.window = 60.0
        self._slots: list[float] = []
        self._lock = threading.Lock()
        self._n = 0
        self.waits = 0

    def acquire(self) -> float:
        # Tính chỗ rồi NGỦ NGOÀI LOCK: giữ lock khi sleep sẽ biến
        # --concurrency > 1 thành tuần tự (thread khác chờ nhau vô ích).
        while True:
            with self._lock:
                now = time.time()
                self._slots = [t for t in self._slots if now - t < self.window]
                if len(self._slots) < self.batch:
                    self._slots.append(now)
                    self._n += 1
                    return now
                sleep_for = self.window - (now - self._slots[0])
                self.waits += 1
                print(f"    [governor] cửa sổ#{self._n}: đã phát {len(self._slots)}"
                      f" câu, nghỉ {sleep_for:.1f}s (trần {self.batch}/phút)", flush=True)
            time.sleep(max(0.05, sleep_for))


def post_chat(base: str, token: str, payload: dict, timeout_s: int):
    """Một lượt /chat, KHÔNG xoay provider: lỗi provider phải hiện đúng là lỗi
    của provider đó, provider khác cứu thì so sánh giữa các provider thành vô
    nghĩa. Trả (data | None, lỗi, giây)."""
    t0 = time.time()
    try:
        r = requests.post(f"{base}/chat", json=payload, headers=auth(token),
                          timeout=timeout_s)
    except requests.exceptions.RequestException as e:
        return None, f"client {type(e).__name__}: {str(e)[:140]}", time.time() - t0
    try:
        body = r.json()
    except ValueError:
        body = None
    secs = time.time() - t0
    if body and body.get("status"):
        data = body["data"]
        data["_provider"] = payload.get("provider", "")
        return data, "", secs
    return None, f"HTTP {r.status_code}: {str(body or r.text)[:200]}", secs


def open_session(base: str, token: str, provider: str, category_ids: list[int]):
    r = requests.post(f"{base}/conversations",
                      json={"category_ids": category_ids, "provider": provider},
                      headers=auth(token), timeout=60)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"mở phiên ({provider}): {r.status_code} {body}")
    return body["data"]["id"]


def upload_any(base: str, token: str, path: Path, category_id: int) -> int:
    """Upload 1 fixture bất kỳ theo đuôi file. doc_type=laws chỉ dán cho tài
    liệu quy định để nhánh compile tree không ăn nhầm bảng lương."""
    import mimetypes
    ctype = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
    with path.open("rb") as fh:
        r = requests.post(f"{base}/uploads/presign",
                          files={"file": (path.name, fh, ctype)},
                          headers=auth(token), timeout=120)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"presign {path.name}: {body}")
    item = body["data"]
    with path.open("rb") as fh:
        put = requests.put(item["upload_url"], data=fh,
                           headers={"Content-Type": item["content_type"]}, timeout=300)
    if put.status_code >= 400:
        raise SystemExit(f"upload {path.name}: {put.status_code} {put.text[:200]}")
    payload = {"name": f"[x] {path.stem}", "content_type": item["content_type"],
               "category_id": category_id, "storage_key": item["key"]}
    if "quy_dinh" in path.name.lower() or "quy_che" in path.name.lower():
        payload["doc_type"] = "laws"
    r = requests.post(f"{base}/articles", json=payload, headers=auth(token), timeout=60)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"tạo bài {path.name}: {body}")
    return body["data"]["id"]


def selftest() -> int:
    """Kiểm tra chính bộ chấm: không cần server, không cần LLM, chạy trong
    chưa đầy 1 giây. Ý tưởng: một bộ rubric chưa từng được test thì chưa
    từng được chứng minh là chấm đúng — đưa trước 2 câu trả lời mẫu (một
    chuyên nghiệp, một kiểu 'máy tra') và buộc verdict phải ra đúng chiều."""
    cites = [{"n": 1, "snippet": "PQ-01 | Cấp bản sao văn bản | 8000 | Bản/lần"},
             {"n": 2, "snippet": "Bãi bỏ toàn bộ quy chế phiên bản 1 kể từ 01/10/2025"},
             {"n": 3, "snippet": "PQ-01 | Cấp bản sao văn bản | 5000 | Bản/lần"}]
    expert = {"answer": ("Mức phí hiện hành là **8.000 VNĐ/bản/lần**[^1]. "
                          "Mức 5.000 VNĐ[^3] của quy chế cũ không còn áp dụng vì "
                          "bản 2.0 bãi bỏ toàn bộ từ 01/10/2025[^2]."),
              "cites": cites, "question_type": "lookup", "_provider": "nvidia"}
    sloppy = {"answer": ("Dựa trên kết quả tìm kiếm, tôi hy vọng thông tin hữu ích. "
                         "Phí là 12345 VNĐ. Cảm ơn bạn đã đặt câu hỏi rất hay!"),
              "cites": [], "question_type": "", "_provider": "ollama"}
    dangling = {"answer": "Mức phí là 8.000 VNĐ[^7].", "cites": cites,
                "question_type": "lookup", "_provider": "nvidia"}
    task = {"expected": "8.000 VNĐ/bản/lần", "kind": "single"}
    # Batch thật lộ hai chỗ bộ chấm trước đây báo sai: dấu thập phân phẩy/chấm
    # (X-03) và biến thể nội bộ tool lọt vào answer (X-01).
    dec = {"answer": "NV01 làm việc 22.5 ngày công.", "cites": [],
           "question_type": "lookup", "_provider": "nvidia"}
    dec_task = {"expected": "NV01 có 22,5 ngày công", "kind": "single"}
    leak = {"answer": "File có 3 sheet[^1] (sheet_count = 3).", "cites": cites,
            "question_type": "lookup", "_provider": "nvidia"}

    cases = [
        ("chuyên gia -> L0 sạch", check_layer0(expert, "nvidia") == []),
        ("chuyên gia -> đúng số", check_layer1(task, expert, "expected")["factual"] == "PASS"),
        ("chuyên gia -> không có số lạ",
         check_layer1(task, expert, "expected")["unsourced"] == []),
        ("chuyên gia -> đủ 6 tiêu chí hình thức", check_layer2(task, expert)["shape"] == "6/6"),
        ("máy tra -> lỗi provider xoay", "PROVIDER_DRIFT:ollama" in check_layer0(sloppy, "nvidia")),
        ("máy tra -> không cite", "NO_CITES" in check_layer0(sloppy, "nvidia")),
        ("máy tra -> rớt hình thức", int(check_layer2(task, sloppy)["shape"].split("/")[0]) <= 2),
        ("máy tra -> số bịa bị đánh dấu",
         "12345" in check_layer1(task, sloppy, "expected")["unsourced"]),
        ("marker mồ côi -> DANGLING", any(e.startswith("DANGLING_MARKER")
                                         for e in check_layer0(dangling, "nvidia"))),
        ("answer rỗng -> EMPTY", check_layer0({"answer": "", "cites": cites}, "nvidia") == ["EMPTY_ANSWER"]),
        ("parse pair", len(parse_pairs(CASES_DIR / "context_test.md")) == 4),
        ("thập phân 22,5 == 22.5", grade(dec_task, dec) == "PASS"),
        ("21.5 không bị coi là số bịa",
         check_layer1({"expected": "", "kind": "single"},
                      {"answer": "NV03 có 21.5 ngày công[^1]",
                       "cites": [{"n": 1, "snippet": "NV03 | 21,5 ngày công"}],
                       "question_type": "lookup", "_provider": "nvidia"},
                      "expected")["unsourced"] == []),
        ("sheet_count bị bắt là lộ nội bộ",
         "no_plumbing_leak" in check_layer2(task, leak)["failed"]),
    ]
    bad = [name for name, ok in cases if not ok]
    for name, ok in cases:
        print(f"  {'ok  ' if ok else 'FAIL'} {name}")
    if bad:
        print(f"\nTỰ TEST BỘ CHẤM SÓT {len(bad)} mục: {bad}")
        return 1
    print(f"\nTỰ TEST BỘ CHẤM: đạt {len(cases)}/{len(cases)} mục "
          "(không gọi LLM nào).")
    return 0


# -------------------------------------------------------------------- eval

def eval_task(base, token, task, provider, category_ids, args, session_id) -> list[dict]:
    """Chạy một task (1 câu, hoặc 1 cặp hai lượt chung phiên) và chấm 3 lớp."""
    rows = []
    turns = ([("q1", "a1"), ("q2", "a2")] if task["kind"] == "pair" else [("question", "expected")])
    for i, (qkey, akey) in enumerate(turns):
        payload = {"message": task[qkey], "category_ids": category_ids}
        if session_id:
            # Có conversation_id thì KHÔNG gửi history: server là nguồn duy
            # nhất, đó chính là điều cặp pair cần chứng minh.
            payload["conversation_id"] = session_id
        else:
            payload["provider"] = provider
        gov = args["governor"]
        gov.acquire()
        data, err, secs = post_chat(base, token, payload, args["chat_timeout"])
        sub = {"id": task["id"] + (f"/{i + 1}" if task["kind"] == "pair" else ""),
               "provider": provider, "answered_by": (data or {}).get("_provider", ""),
               "question": task[qkey], "expected": task.get(akey, ""),
               "conversation_id": session_id, "secs": round(secs, 1),
               "n_cites": len((data or {}).get("cites") or []),
               "answer": (data or {}).get("answer", ""),
               # Lưu nguyên cites để báo cáo chấm lại được offline: sửa rubric
               # thì khỏi phải gọi lại LLM một lần nữa.
               "cites": (data or {}).get("cites") or [],
               "error": err}
        if err or data is None:
            sub["l0"] = ["ERROR"]
            sub["verdict"] = "ERROR"
        else:
            l0 = check_layer0(data, provider if not session_id else "")
            sub["l0"] = l0
            sub["l1"] = check_layer1(task, data, akey)
            sub["l2"] = check_layer2(task, data)
            sub["verdict"] = ("FAIL0" if l0 else
                              "FAIL1" if sub["l1"]["factual"].startswith("FAIL") else
                              "PASS")
        rows.append(sub)
        print(f"  [{sub['id']:<26}] {provider:<10} {sub['verdict']:<6} "
              f"{sub['secs']:6.1f}s cites={sub['n_cites']} "
              f"shape={sub.get('l2', {}).get('shape', '-')} "
              f"{sub['error']}", flush=True)
    return rows


def summarize(rows: list[dict]) -> dict:
    per: dict[str, dict] = {}
    for r in rows:
        s = per.setdefault(r["provider"], {"n": 0, "pass": 0, "fail0": 0, "fail1": 0,
                                           "error": 0, "with_cites": 0, "secs": 0.0,
                                           "shape": [], "unsourced": 0})
        s["n"] += 1
        key = {"PASS": "pass", "FAIL0": "fail0", "FAIL1": "fail1", "ERROR": "error"}[r["verdict"]]
        s[key] += 1
        if r["n_cites"]:
            s["with_cites"] += 1
        s["secs"] += r["secs"]
        if r.get("l2"):
            ok, total = r["l2"]["shape"].split("/")
            s["shape"].append(int(ok) / int(total))
        s["unsourced"] += len(r.get("l1", {}).get("unsourced", []))
    for s in per.values():
        s["shape_avg"] = round(sum(s["shape"]) / len(s["shape"]), 2) if s["shape"] else 0
        s["avg_secs"] = round(s["secs"] / s["n"], 1)
    return per


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--base", default="http://127.0.0.1:9001/api/v1")
    ap.add_argument("--providers", nargs="*", default=["9router", "nvidia", "openrouter"],
                    help="luân phiên theo task; rỗng = default cấu hình BE")
    ap.add_argument("--file", default=str(CASES_DIR / "test_case.md"))
    ap.add_argument("--docs", nargs="*", default=None,
                    help="fixture cần upload+index cho user eval (mặc định 2 PDF quy định)")
    ap.add_argument("--username", default="", help="login bằng user có sẵn (mặc định: đăng ký user mới)")
    ap.add_argument("--password", default="secret123")
    ap.add_argument("--skip-upload", action="store_true")
    ap.add_argument("--category-id", type=int, default=0)
    ap.add_argument("--max", type=int, default=12, help="tối đa N task (0 = hết)")
    ap.add_argument("--batch-size", type=int, default=4,
                    help="số lượt /chat mỗi cửa sổ 60s — xem docstring, đừng tăng")
    ap.add_argument("--rpm", type=int, default=40, help="trần self-protect của provider")
    ap.add_argument("--concurrency", type=int, default=1,
                    help="1..4; >1 thì mỗi task một phiên riêng để lịch sử không lẫn")
    ap.add_argument("--chat-timeout", type=int, default=300)
    ap.add_argument("--wait-index", type=int, default=900, help="chờ index tối đa (s)")
    ap.add_argument("--out", default=str(HERE / "expert_eval_results.json"))
    ap.add_argument("--selftest", action="store_true",
                    help="chỉ kiểm tra bộ chấm 3 lớp trên câu trả lời mẫu, không gọi server")
    args = ap.parse_args()

    if args.selftest:
        sys.exit(selftest())

    concurrency = max(1, min(args.concurrency, 4))
    if concurrency != args.concurrency:
        print(f"cảnh báo: --concurrency {args.concurrency} kẹp về {concurrency} "
              f"(mỗi /chat tự nó đã là 8-10 lượt LLM)")
    batch = max(1, min(args.batch_size, args.rpm))
    if args.batch_size > args.rpm:
        print(f"cảnh báo: --batch-size {args.batch_size} > --rpm {args.rpm}, hạ về {batch}. "
              f"Provider rpm thấp (groq 30) thì tự hạ batch-size lấy.")

    providers = args.providers or [""]
    tasks = load_tasks(Path(args.file), args.max)
    if not tasks:
        raise SystemExit(f"không parse được task nào từ {args.file}")

    # User eval mới tinh: bài upload + category + phiên chat nằm gọn một chỗ,
    # không lẫn dữ liệu user khác (bài học Đợt 24: vector theo user_id).
    username = args.username or f"ev{uuid.uuid4().hex[:8]}"
    register(args.base, username, args.password)
    token = login(args.base, username, args.password)
    print(f"user={username} base={args.base} — {len(tasks)} task, "
          f"providers={' '.join(providers)}")

    if args.skip_upload:
        category_ids = [args.category_id]
        print(f"dùng category {category_ids} (giả định đã có vector cho user này)")
    else:
        docs = [Path(d) for d in (args.docs or [str(d) for d in DEFAULT_DOCS])]
        cat_id = create_category(args.base, token, f"X {uuid.uuid4().hex[:6]}")
        created = []
        for d in docs:
            if not d.exists():
                raise SystemExit(f"thiếu fixture {d} — chạy python make_complex_fixtures.py")
            created.append(upload_any(args.base, token, d, cat_id))
        for aid in created:
            wait_index_done(args.base, token, aid, args.wait_index, 10)
        category_ids = [cat_id]
        print(f"category = {cat_id} (chạy tiếp: --skip-upload --category-id {cat_id})")

    gov = Governor(batch, args.rpm)
    ctx = {"governor": gov, "chat_timeout": args.chat_timeout}
    sessions: dict[str, int] = {}
    lock = threading.Lock()

    # Vòng xoay provider theo task (giống e2e_testcases): index của task trong
    # danh sách quyết định provider, chạy lại vẫn ra đúng thứ tự.
    def session_for(provider: str) -> int:
        if concurrency > 1:
            # Mỗi task một phiên: hai luồng hỏi đáp xen nhau trong cùng phiên thì
            # lịch sử nạp lên lẫn lộn, phép thử "server là nguồn lịch sử" mất nghĩa.
            return open_session(args.base, token, provider, category_ids)
        with lock:
            if provider not in sessions:
                sessions[provider] = open_session(args.base, token, provider, category_ids)
                print(f"  phiên của {provider or 'default'}: #{sessions[provider]}")
            return sessions[provider]

    def one(item):
        idx, task = item
        provider = providers[idx % len(providers)]
        sid = session_for(provider)
        return eval_task(args.base, token, task, provider, category_ids, ctx, sid)

    rows: list[dict] = []
    t0 = time.time()
    work = list(enumerate(tasks))
    if concurrency == 1:
        for item in work:
            rows.extend(one(item))
    else:
        with ThreadPoolExecutor(max_workers=concurrency) as pool:
            for part in pool.map(one, work):
                rows.extend(part)

    per = summarize(rows)
    Path(args.out).write_text(json.dumps(
        {"meta": {"base": args.base, "user": username, "category_ids": category_ids,
                  "sessions": sessions, "providers": providers, "batch": batch,
                  "rpm": args.rpm, "concurrency": concurrency, "file": str(args.file),
                  "governor_waits": gov.waits, "total_secs": round(time.time() - t0, 1)},
         "rows": rows}, ensure_ascii=False, indent=1), encoding="utf-8")

    print("\n==== TỔNG HỢP THEO PROVIDER ====")
    print(f"{'provider':<12} {'n':>3} {'pass':>5} {'fail0':>6} {'fail1':>6} "
          f"{'err':>4} {'cites>0':>8} {'shape':>6} {'unsrc':>6} {'avg_s':>6}")
    for p, s in sorted(per.items()):
        print(f"{p or 'default':<12} {s['n']:>3} {s['pass']:>5} {s['fail0']:>6} "
              f"{s['fail1']:>6} {s['error']:>4} {s['with_cites']:>8} "
              f"{s['shape_avg']:>6} {s['unsourced']:>6} {s['avg_secs']:>6}")
    print(f"\nbáo cáo: {args.out} | governor nghỉ {gov.waits} lần trong "
          f"{time.time() - t0:.0f}s | phiên: {sessions or '(mỗi task một phiên)'}")
    bad = [r for r in rows if r["verdict"] in ("ERROR", "FAIL0")]
    for r in bad[:12]:
        print(f"  ! {r['id']} [{r['provider']}] {r['verdict']} "
              f"l0={r['l0']} err={r['error'][:120]}")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
