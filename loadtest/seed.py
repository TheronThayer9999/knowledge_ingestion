"""Seed dữ liệu thật cho load test: N user, mỗi user vài danh mục + bài riêng.

Chạy:  python seed.py --base http://127.0.0.1:9001/api/v1 --users 10

Đầu ra: tokens.json (token + id của từng user, và danh sách article theo user)
để k6/vegeta dùng lại — không login lại trong vòng lặp tải (login là bcrypt,
sẽ đo nhầm thời gian của endpoint cần đo).
"""

import argparse
import json
import mimetypes
import random
import time
import uuid
from pathlib import Path

import requests

FIXTURES = Path(__file__).resolve().parent / "fixtures"
# Bộ file theo thứ tự từ nhỏ đến lớn — seed tuần tự để không upload 6MB lúc
# đang đo độ trễ của request khác.
FILES = [
    ("pdf_text_layer.pdf", "application/pdf"),
    ("pdf_scan.pdf", "application/pdf"),
    ("docx_viet.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"),
    ("txt_nhieu_dieu.txt", "text/plain"),
    ("rong.txt", "text/plain"),
    ("txt_noi_dung.txt", "text/plain"),
]
CATEGORY_POOL = [
    "Nhân sự", "Tài chính", "Pháp chế", "Vận hành", "Bảo mật",
    "Kỹ thuật", "Khách hàng", "Mua sắm",
]


def register(base, username, password):
    r = requests.post(f"{base}/users", json={
        "username": username, "password": password,
        "email": f"{username}@example.com",
    }, timeout=30)
    # 400 "đã tồn tại" là chấp nhận được — seed chạy lại được.
    if r.status_code >= 400:
        body = r.json()
        if body.get("code") == 0:
            return
        raise SystemExit(f"register {username}: {r.status_code} {body}")
    return


def login(base, username, password):
    r = requests.post(f"{base}/auth/login",
                     json={"username": username, "password": password}, timeout=30)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"login {username}: {body}")
    return body["data"]["token"]


def auth(token):
    return {"Authorization": f"Bearer {token}"}


def create_category(base, token, name):
    r = requests.post(f"{base}/categories", json={"name": name},
                      headers=auth(token), timeout=30)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"create category {name}: {body}")
    return body["data"]["id"]


def presign_and_upload(base, token, path, content_type):
    """Đúng 3 bước của luồng thật: presign -> PUT thẳng lên kho -> giữ key."""
    with path.open("rb") as fh:
        # Chỉ gửi tên + vài byte đầu: handler chỉ đọc 512B sniff rồi ngắt,
        # nhưng requests vẫn gửi hết — ở đây để đơn giản, file nhỏ.
        r = requests.post(f"{base}/uploads/presign",
                          files={"file": (path.name, fh, content_type)},
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
    return item["key"]


def create_article(base, token, category_id, name, content_type, storage_key=None, url=None):
    payload = {"name": name, "content_type": content_type, "category_id": category_id}
    if storage_key:
        payload["storage_key"] = storage_key
    if url:
        payload["url"] = url
    r = requests.post(f"{base}/articles", json=payload,
                      headers=auth(token), timeout=60)
    body = r.json()
    return r.status_code, body


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:9001/api/v1")
    ap.add_argument("--users", type=int, default=10)
    ap.add_argument("--password", default="secret123")
    ap.add_argument("--files-per-user", type=int, default=3,
                    help="mỗi user đăng ký mấy bài (xin key từ kho, tốn thời gian)")
    args = ap.parse_args()

    run_id = uuid.uuid4().hex[:6]
    users = []
    t0 = time.time()

    for i in range(args.users):
        # Dữ liệu KHÁC NHAU mỗi user: khác danh mục, khác số bài, khác file.
        # Nếu mọi user giống nhau thì load test đo được cache ấm và che mất
        # chỗ dữ liệu lệch nhau làm nghẽn.
        username = f"lt{run_id}u{i:02d}"
        register(args.base, username, args.password)
        token = login(args.base, username, args.password)

        n_cat = random.choice([1, 2, 3])
        cats = [create_category(args.base, token, f"{c} u{i}")
                for c in random.sample(CATEGORY_POOL, n_cat)]

        n_files = random.randint(1, args.files_per_user + 1)
        picks = random.sample(FILES, min(n_files, len(FILES)))
        articles = []
        for fname, ctype in picks:
            path = FIXTURES / fname
            key = presign_and_upload(args.base, token, path, ctype)
            status, body = create_article(
                args.base, token, random.choice(cats),
                f"[u{i}] {fname} {run_id}", ctype, storage_key=key)
            articles.append({"id": (body.get("data") or {}).get("id"),
                             "name": fname, "create_status": status,
                             "create_ok": bool(body.get("status")),
                             "create_msg": body.get("message")})

        # 1 bài URL (không blob) để phủ nhánh article không có storage.
        url = f"https://example.com/docs/{username}/{run_id}"
        status, body = create_article(
            args.base, token, cats[0], f"[u{i}] bài URL", "text/html", url=url)
        articles.append({"id": (body.get("data") or {}).get("id"),
                         "name": "<url>", "create_status": status,
                         "create_ok": bool(body.get("status"))})

        users.append({"username": username, "token": token,
                      "categories": cats, "articles": articles})
        print(f"  user {i:02d}: {len(cats)} danh mục, {len(articles)} bài", flush=True)

    out = Path(__file__).resolve().parent / "tokens.json"
    out.write_text(json.dumps({"base": args.base, "run_id": run_id,
                               "users": users}, ensure_ascii=False, indent=1),
                   encoding="utf-8")
    print(f"\n{len(users)} user, {sum(len(u['articles']) for u in users)} bài "
          f"trong {time.time() - t0:.1f}s -> {out.name}")


if __name__ == "__main__":
    main()