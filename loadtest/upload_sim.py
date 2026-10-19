"""Mô phỏng nhiều user upload dữ liệu thật, song song.

Mỗi user: đăng ký, đăng nhập, tạo 1-3 danh mục, rồi upload N file ~100MB theo
đúng luồng thật: presign (gửi file qua multipart để server đọc kích thước) ->
PUT thẳng lên kho S3 bằng URL đã ký -> tạo bài. Chỉ `--index-sample` file đầu
của mỗi user được tạo thành bài để worker index; phần còn lại chỉ nằm trong kho
(đo tải upload, không kéo theo index 7 ngày).

Chạy:
  python upload_sim.py --users 10 --files 10 --size-mb 100 --workers 5

Đầu ra: loadtest/upload_results.json (độ trễ từng bước, lỗi) và
loadtest/tokens_upload.json (token + id để k6/vegeta dùng lại).
"""

import argparse
import json
import mimetypes
import random
import statistics
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

import requests
from requests_toolbelt import MultipartEncoder

from seed import CATEGORY_POOL, create_article, create_category, login, register

HERE = Path(__file__).resolve().parent
BIG = HERE / "fixtures_big"

# Từ vựng hành chính/pháp lý tiếng Việt có dấu — văn bản nội bộ thật trông như vậy.
WORDS = (
    "công ty người lao động hợp đồng quy định nhân viên quyền lợi nghĩa vụ "
    "thời gian làm việc lương thưởng bảo hiểm xã hội nghỉ phép năm trách nhiệm "
    "quản lý bộ phận kế toán tài chính thanh toán hóa đơn chứng từ phê duyệt "
    "quy trình kiểm soát rủi ro bảo mật dữ liệu khách hàng hệ thống vận hành "
    "báo cáo định kỳ đánh giá hiệu quả mục tiêu kế hoạch ngân sách mua sắm "
    "nhà cung cấp hợp đồng thầu chất lượng an toàn lao động phòng ban Hà Nội "
    "Thành phố Hồ Chí Minh Đà Nẵng xử lý khiếu nại đào tạo tuyển dụng"
).split()


def gen_text_file(path: Path, target_bytes: int, seed: int) -> None:
    """Sinh file text tiếng Việt có dòng, có 'Điều N.' — không lặp đoạn."""
    rnd = random.Random(seed)
    written = 0
    article_no = 1
    with path.open("w", encoding="utf-8", newline="\n") as fh:
        while written < target_bytes:
            parts = []
            for _ in range(rnd.randint(3, 6)):
                n = rnd.randint(8, 22)
                sent = " ".join(rnd.choice(WORDS) for _ in range(n))
                parts.append(sent[:1].upper() + sent[1:] + ".")
            line = " ".join(parts)
            if rnd.random() < 0.15:
                line = f"Điều {article_no}. " + line
                article_no += 1
            line += "\n"
            fh.write(line)
            written += len(line.encode("utf-8"))


def ensure_fixtures(users: int, files: int, size_mb: int) -> list[Path]:
    """Sinh trước toàn bộ file (tái sử dụng nếu đã có đúng kích thước)."""
    BIG.mkdir(exist_ok=True)
    out = []
    target = size_mb * 1024 * 1024
    for u in range(users):
        for j in range(files):
            p = BIG / f"u{u:02d}_f{j:02d}.txt"
            # Kích thước lệch ±10% quanh size_mb để không đều nhau hoàn toàn.
            jitter = random.Random(u * 1000 + j).uniform(0.9, 1.1)
            want = int(target * jitter)
            if not p.exists() or abs(p.stat().st_size - want) > 2 * 1024 * 1024:
                gen_text_file(p, want, seed=u * 1000 + j)
            out.append(p)
    return out


class Timer:
    def __init__(self):
        self.samples: dict[str, list[float]] = {}
        self.errors: list[dict] = []

    def add(self, step: str, seconds: float) -> None:
        self.samples.setdefault(step, []).append(seconds)

    def err(self, step: str, detail: str) -> None:
        self.errors.append({"step": step, "detail": detail[:300]})


def timed_presign_and_put(base: str, token: str, path: Path, timer: Timer, ctype: str):
    headers = {"Authorization": f"Bearer {token}"}
    t = time.time()
    with path.open("rb") as fh:
        enc = MultipartEncoder(fields={"file": (path.name, fh, ctype)})
        r = requests.post(f"{base}/uploads/presign", data=enc,
                          headers={**headers, "Content-Type": enc.content_type},
                          timeout=900)
    timer.add("presign", time.time() - t)
    body = r.json() if r.headers.get("content-type", "").startswith("application/json") else {}
    if r.status_code >= 400 or not body.get("status"):
        timer.err("presign", f"{path.name} {r.status_code} {str(body)[:200]}")
        return None
    item = body["data"]

    t = time.time()
    with path.open("rb") as fh:
        put = requests.put(item["upload_url"], data=fh,
                           headers={"Content-Type": item["content_type"]}, timeout=900)
    timer.add("put", time.time() - t)
    if put.status_code >= 400:
        timer.err("put", f"{path.name} {put.status_code} {put.text[:200]}")
        return None
    return item["key"]


def run_user(u: int, base: str, files: list[Path], index_sample: int,
             password: str, run_id: str) -> dict:
    timer = Timer()
    username = f"up{run_id}u{u:02d}"
    article_files = [HERE / "fixtures" / "txt_nhieu_dieu.txt"]
    register(base, username, password)
    token = login(base, username, password)

    rnd = random.Random(u)
    cats = [create_category(base, token, f"{c} {run_id}u{u}")
            for c in rnd.sample(CATEGORY_POOL, rnd.choice([1, 2, 3]))]

    uploaded, articles = [], []
    for idx, path in enumerate(files):
        t0 = time.time()
        ctype = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        key = timed_presign_and_put(base, token, path, timer, ctype)
        if key is None:
            continue
        uploaded.append({"file": path.name, "bytes": path.stat().st_size,
                         "key": key, "seconds": round(time.time() - t0, 2)})

    # Chỉ file nhỏ mới thành bài để index: index bge-m3 ~7.8 chunk/s, file 100MB
    # là hàng chục nghìn chunk, index hết sẽ mất nhiều ngày.
    for path in article_files[:index_sample]:
        ctype = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        key = timed_presign_and_put(base, token, path, timer, ctype)
        if key is None:
            continue
        t = time.time()
        status, body = create_article(base, token, rnd.choice(cats),
                                      f"[{username}] {path.name}", ctype,
                                      storage_key=key)
        timer.add("create_article", time.time() - t)
        if not body.get("status"):
            timer.err("create_article", f"{path.name} {status} {str(body)[:200]}")
        else:
            articles.append({"id": body["data"]["id"], "name": path.name})

    return {"username": username, "token": token, "categories": cats,
            "uploaded": uploaded, "articles": articles,
            "timer_samples": timer.samples, "errors": timer.errors}


def pct(xs: list[float], p: float) -> float:
    if not xs:
        return 0.0
    xs = sorted(xs)
    k = min(len(xs) - 1, int(round(p / 100 * (len(xs) - 1))))
    return xs[k]


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:9001/api/v1")
    ap.add_argument("--users", type=int, default=10)
    ap.add_argument("--files", type=int, default=10)
    ap.add_argument("--size-mb", type=int, default=100)
    ap.add_argument("--workers", type=int, default=5, help="số user upload song song")
    ap.add_argument("--index-sample", type=int, default=1,
                    help="mỗi user tạo bài cho mấy file đầu để index")
    ap.add_argument("--password", default="secret123")
    ap.add_argument("--file", help="dùng file thật này thay cho file sinh sẵn (mọi slot đều upload file này)")
    ap.add_argument("--real-pdf", help="PDF thật thay slot đầu của mỗi user; các slot còn lại là txt sinh sẵn")
    args = ap.parse_args()

    print("Chuẩn bị file...", flush=True)
    t0 = time.time()
    if args.file:
        real = Path(args.file)
        all_files = [real] * (args.users * args.files)
    else:
        all_files = ensure_fixtures(args.users, args.files, args.size_mb)
        if args.real_pdf:
            pdf = Path(args.real_pdf)
            for u in range(args.users):
                all_files[u * args.files] = pdf
    total_bytes = sum(p.stat().st_size for p in all_files)
    print(f"  {len(all_files)} file, {total_bytes / 1e9:.2f} GB, {time.time() - t0:.0f}s", flush=True)

    run_id = uuid.uuid4().hex[:6]
    results, t_start = [], time.time()
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        futs = {pool.submit(run_user, u, args.base, all_files[u * args.files:(u + 1) * args.files],
                            args.index_sample, args.password, run_id): u
                for u in range(args.users)}
        for f in as_completed(futs):
            res = f.result()
            results.append(res)
            print(f"  {res['username']}: {len(res['uploaded'])} file lên kho, "
                  f"{len(res['articles'])} bài, {len(res['errors'])} lỗi", flush=True)
    elapsed = time.time() - t_start

    samples: dict[str, list[float]] = {}
    errors, uploaded_bytes, n_uploaded = [], 0, 0
    for r in results:
        for step, xs in r["timer_samples"].items():
            samples.setdefault(step, []).extend(xs)
        errors.extend(r["errors"])
        for up in r["uploaded"]:
            uploaded_bytes += up["bytes"]
            n_uploaded += 1

    summary = {
        "run_id": run_id, "users": args.users, "files_per_user": args.files,
        "files_uploaded": n_uploaded, "bytes_uploaded": uploaded_bytes,
        "elapsed_s": round(elapsed, 1),
        "throughput_MBps": round(uploaded_bytes / 1024 / 1024 / elapsed, 1),
        "errors": len(errors),
        "latency_s": {step: {"n": len(xs), "p50": round(pct(xs, 50), 2),
                             "p95": round(pct(xs, 95), 2), "max": round(max(xs), 2),
                             "mean": round(statistics.mean(xs), 2)}
                      for step, xs in samples.items()},
    }
    out = HERE / "upload_results.json"
    out.write_text(json.dumps({"summary": summary, "errors": errors, "users": [
        {k: v for k, v in r.items() if k not in ("timer_samples", "errors")} for r in results]},
        ensure_ascii=False, indent=1), encoding="utf-8")
    (HERE / "tokens_upload.json").write_text(json.dumps({"base": args.base, "run_id": run_id,
        "users": [{"username": r["username"], "token": r["token"], "categories": r["categories"],
                   "articles": r["articles"]} for r in results]}, ensure_ascii=False, indent=1),
        encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
