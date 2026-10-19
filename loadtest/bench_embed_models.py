"""So sánh throughput embed giữa các model có sẵn trên máy.

Đo steady-state (bỏ qua lần nạp model đầu) và theo đúng kích thước chunk mà
pipeline thật sự cắt ra, vì "chunk/s" không có nghĩa nếu chunk nhỏ hơn.
"""

import json
import sys
import time
import urllib.request

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

URL = "http://localhost:11434/api/embed"
BATCH = 32

# Chunk ~1000 ký tự — khớp worker.chunker.size mặc định.
sample = ("Điều khoản này quy định rõ ràng về chế độ làm việc của nhân viên "
          "chính thức trong công ty, áp dụng từ ngày ban hành. ") * 8


def embed(model, texts):
    body = json.dumps({"model": model, "input": texts}).encode()
    req = urllib.request.Request(URL, data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=1800) as r:
        return json.loads(r.read())


def bench(model, rounds=3):
    embed(model, [sample])          # warm-up, không tính
    best, dim = None, None
    for _ in range(rounds):
        t0 = time.time()
        out = embed(model, [sample] * BATCH)
        dt = time.time() - t0
        dim = len(out["embeddings"][0])
        rate = BATCH / dt
        best = rate if best is None else max(best, rate)
    return best, dim


def main():
    models = ["bge-m3:latest", "nomic-embed-text:latest"]
    print(f"chunk = {len(sample):,} ký tự, batch = {BATCH}\n")
    print(f"{'model':<26}{'dim':>6}{'chunk/s':>10}{'KB/s':>9}{'giờ / 10GB':>13}")
    print("-" * 66)
    rows = []
    for m in models:
        try:
            rate, dim = bench(m)
        except Exception as e:                       # noqa: BLE001
            print(f"{m:<26} lỗi: {e}")
            continue
        chunks = 10 * 1024 ** 3 / len(sample)         # 10GB text
        hours = chunks / rate / 3600
        kb = BATCH * len(sample) / (3600 / rate) / 1024
        print(f"{m:<26}{dim:>6}{rate:>10.2f}{kb:>9.0f}{hours:>13,.0f}")
        rows.append((m, rate, hours))
    if len(rows) == 2:
        a, b = rows
        print(f"\n{b[0]} nhanh gấp {b[1] / a[1]:.1f}x so với {a[0]}, "
              f"tiết kiệm {a[2] - b[2]:,.0f} giờ trên 10GB.")
        print("Đổi lại: nomic ưu tiên tiếng Anh, dim 768; bge-m3 đa ngôn ngữ, "
              "tiếng Việt tốt hơn.")


if __name__ == "__main__":
    main()