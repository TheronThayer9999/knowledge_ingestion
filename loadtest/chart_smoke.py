"""Smoke test BE chart_from_cite: 1 luot /chat that noi "ve bieu do" tren fixture
bang luong da index (category 130, user cua batch Dot 25). In ra charts payload
de doi chieu so voi chunk da dan.

Chay: python loadtest/chart_smoke.py
"""
import json
import sys

import requests

sys.path.insert(0, "loadtest")
from e2e_testcases import auth, login  # noqa: E402

BASE = "http://127.0.0.1:9001/api/v1"
USERNAME = "eva344856d"
PASSWORD = "secret123"
CATEGORY_ID = 130
MESSAGE = "Vẽ biểu đồ cột cho tôi xem phân bố phụ cấp của nhân viên"


def main() -> int:
    token = login(BASE, USERNAME, PASSWORD)
    # Mở phiên để kiểm luôn bước lưu: charts phải xuống messages.charts và về
    # nguyên khi đọc lại (mở lại phiên vẫn thấy biểu đồ).
    conv = requests.post(f"{BASE}/conversations", json={"category_ids": [CATEGORY_ID]},
                         headers=auth(token), timeout=60).json()["data"]["id"]
    r = requests.post(
        f"{BASE}/chat",
        json={"message": MESSAGE, "category_ids": [CATEGORY_ID], "conversation_id": conv},
        headers=auth(token),
        timeout=300,
    )
    print("http", r.status_code)
    body = r.json()
    data = body.get("data") or {}
    print("answer:\n", (data.get("answer") or "")[:1200])
    print("\ncites:", json.dumps(data.get("cites"), ensure_ascii=False)[:900])
    charts = data.get("charts")
    print("\ncharts is None?", charts is None)
    print("charts:", json.dumps(charts, ensure_ascii=False, indent=2)[:2000])

    msgs = requests.get(f"{BASE}/conversations/{conv}/messages", headers=auth(token),
                        timeout=60).json().get("data") or []
    stored = [m.get("charts") for m in msgs if m.get("charts")]
    print("\nsố message mang charts khi đọc lại phiên:", len(stored))
    print("stored charts:", json.dumps(stored, ensure_ascii=False)[:1200])
    return 0 if isinstance(charts, list) and charts and stored else 1


if __name__ == "__main__":
    raise SystemExit(main())
