"""Đo throughput embed thật của bge-m3 trên máy này.

Cần số thật trước khi quyết scale: "10 user × 10 file × 100MB" nghe thì vừa,
nhưng nếu index toàn bộ thì con số chunk là hàng chục triệu — và CPU embed
mỗi chunk tính bằng giây thì đó là hàng tháng compute.
"""

import json
import sys
import time
import urllib.request

# Console Windows mặc định cp1252, in tiếng Việt ra sẽ vỡ UnicodeEncodeError.
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

URL = "http://localhost:11434/api/embed"
MODEL = "bge-m3:latest"

# Chunk ~1000 ký tự tiếng Việt — gần kích thước thật mà pipeline cắt ra.
sample = ("Điều khoản này quy định rõ ràng về chế độ làm việc của nhân viên "
          "chính thức trong công ty, áp dụng từ ngày ban hành. ") * 20


def embed(texts):
    body = json.dumps({"model": MODEL, "input": texts}).encode()
    req = urllib.request.Request(URL, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=600) as r:
        return json.loads(r.read())


def main():
    print(f"model={MODEL}")
    print(f"1 chunk ~ {len(sample):,} ký tự\n")

    # Lượt đầu: nạp model vào RAM — KHÔNG tính vào throughput.
    t0 = time.time()
    embed([sample])
    print(f"nạp model (warm-up): {time.time() - t0:.1f}s  <- không tính\n")

    for batch in (8, 32):
        texts = [sample] * batch
        t0 = time.time()
        out = embed(texts)
        dt = time.time() - t0
        dim = len(out["embeddings"][0])
        print(f"batch {batch:>3} chunk: {dt:6.2f}s -> "
              f"{batch / dt:6.2f} chunk/s  (dim={dim}, "
              f"{batch * len(sample) / dt / 1024:.0f} KB/s)")

    # Chi phí thật cho 10 user × 10 file × 100MB, giả sử 1000 ký tự/chunk.
    rate = batch / dt
    total_bytes = 10 * 10 * 100 * 1024 * 1024
    chunks = total_bytes / len(sample)
    secs = chunks / rate
    print(f"\n--- suy ra cho {total_bytes / 1024**3:.0f}GB text ---")
    print(f"  chunks ước tính: {chunks:,.0f}")
    print(f"  thời gian embed : {secs / 3600:,.0f} giờ = {secs / 86400:,.0f} ngày")
    print(f"  (chưa tính OCR, chunker, upsert Qdrant)")


if __name__ == "__main__":
    main()