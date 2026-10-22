"""E2E tạm cho Knowledge Compilation phase 1b+1c — nhanh hơn chuỗi lệnh PowerShell.

Chạy toàn bộ luồng thật qua API:
  login -> presign + PUT file quy_che_tree.txt -> tạo bài (doc_type=laws)
  -> POLL /articles/:id/sections đến khi compile_status=done (worker 9router)
  -> GET /articles/:id/tree, KHỎÁi cấu trúc nesting Chương > Mục > Điều
  + mọi node phải có summary. Kiểm luôn cây phẳng của bài --flat-id (mặc định 42).

Chạy:  python e2e_compile_tree.py            (base 9002, user p1a2760b8)
       python e2e_compile_tree.py --base http://127.0.0.1:9001/api/v1
"""

import argparse
import sys
import time
import uuid
from pathlib import Path

import requests

from seed import auth, login, presign_and_upload

HERE = Path(__file__).resolve().parent
FIXTURE = HERE / "fixtures" / "quy_che_tree.txt"

fails: list[str] = []


def check(cond: bool, msg: str) -> None:
    print(("  PASS " if cond else "  FAIL ") + msg)
    if not cond:
        fails.append(msg)


def get(base, token, path):
    r = requests.get(f"{base}{path}", headers=auth(token), timeout=60)
    return r.status_code, r.json()


def create_laws_article(base, token, category_id, name, key):
    payload = {"name": name, "content_type": "text/plain", "category_id": category_id,
               "storage_key": key, "doc_type": "laws"}
    r = requests.post(f"{base}/articles", json=payload, headers=auth(token), timeout=60)
    body = r.json()
    if not body.get("status"):
        raise SystemExit(f"tạo bài lỗi: {body}")
    return body["data"]["id"]


def first_category(base, token):
    code, body = get(base, token, "/categories")
    cats = (body.get("data") or {}).get("categories") or []
    ids = [c["id"] for c in cats if isinstance(c, dict) and c.get("id")]
    if not ids:
        raise SystemExit(f"chưa có danh mục nào để gắn bài (code={code})")
    return ids[0]


def wait_compile_done(base, token, article_id, timeout_s, interval_s):
    """Poll endpoint sections (không đụng DB) đến khi compile xong."""
    t0 = time.time()
    while time.time() - t0 < timeout_s:
        code, body = get(base, token, f"/articles/{article_id}/sections")
        data = body.get("data") or {}
        st = data.get("compile_status")
        print(f"  ... compile_status={st} sections={data.get('section_count')} "
              f"({time.time() - t0:.0f}s)", flush=True)
        if st == "done":
            return data
        if st == "failed":
            raise SystemExit(f"bài {article_id} compile failed: {data}")
        time.sleep(interval_s)
    raise SystemExit(f"hết {timeout_s}s mà bài {article_id} chưa compile xong")


def walk(nodes, depth=0):
    for n in nodes:
        s = (n.get("summary") or "").replace("\n", " ")
        print("    " + "  " * depth + f"- {n['name']}" + (f"  [{s[:60]}...]" if s else "  [KHÔNG SUMMARY]"))
        walk(n.get("children") or [], depth + 1)


def headword(name):
    w = name.split(" ", 1)[0].upper().rstrip(":")
    return w


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:9002/api/v1")
    ap.add_argument("--username", default="p1a2760b8")
    ap.add_argument("--password", default="secret123")
    ap.add_argument("--timeout", type=int, default=420, help="giờ compile tối đa (s)")
    ap.add_argument("--interval", type=float, default=10)
    ap.add_argument("--flat-id", type=int, default=42, help="bài Điều-phẳng để kiểm cây phẳng")
    ap.add_argument("--skip-upload", action="store_true", help="dùng bài --article-id có sẵn, không upload lại")
    ap.add_argument("--article-id", type=int, default=0)
    args = ap.parse_args()

    token = login(args.base, args.username, args.password)
    print(f"login ok, base={args.base}")

    if args.skip_upload:
        article_id = args.article_id
    else:
        key = presign_and_upload(args.base, token, FIXTURE, "text/plain")
        cat = first_category(args.base, token)
        name = f"E2E tree {uuid.uuid4().hex[:6]}"
        article_id = create_laws_article(args.base, token, cat, name, key)
        print(f"đã tạo bài {article_id} ({name}), key={key}")

    print("\n[1] chờ worker extract -> index -> compile (9router)...")
    sections = wait_compile_done(args.base, token, article_id, args.timeout, args.interval)
    check(sections["section_count"] > 0, f"sections>0 (={sections['section_count']})")

    print("\n[2] GET /tree — kiểm nesting + summary...")
    code, body = get(args.base, token, f"/articles/{article_id}/tree")
    check(code == 200 and body.get("status"), f"tree trả 200 (code={code})")
    tree = body.get("data") or {}
    roots = tree.get("roots") or []

    def flatten(nodes, depth=0):
        out = []
        for n in nodes:
            out.append((n, depth))
            out.extend(flatten(n.get("children") or [], depth + 1))
        return out

    all_nodes = flatten(roots)
    check(tree.get("node_count") == len(all_nodes) > 0,
          f"node_count={tree.get('node_count')} khớp tổng node={len(all_nodes)}")

    missing = [n["name"] for n, _ in all_nodes if not (n.get("summary") or "").strip()]
    check(not missing, f"mọi node có summary (thiếu: {missing[:3]})")

    nested = [(n, d) for n, d in all_nodes if (n.get("children") or [])]
    check(len(nested) > 0, "có ít nhất 1 node cha (chương/mục có con)")
    for n, d in nested:
        for c in n["children"]:
            check(headword(c["name"]) in ("MỤC", "ĐIỀU") or d < 0,
                  f"con của {n['name'][:20]!r} là {c['name'][:20]!r} (MỤC/ĐIỀU)")
            # Điều không được có con; MỤC chỉ chứa ĐIỀU.
            if headword(n["name"]) == "MỤC":
                check(headword(c["name"]) == "ĐIỀU", f"MỤC chỉ chứa ĐIỀU, gặp {c['name'][:20]!r}")
            if headword(c["name"]) == "ĐIỀU":
                check(not (c.get("children") or []), "ĐIỀU không có con")
    # Root không được là Điều khi cha Chương/Mục đứng trước nó trong danh mục.
    for n, d in all_nodes:
        if d == 0 and headword(n["name"]) == "ĐIỀU":
            print(f"  NOTE: root là ĐIỀU đơn lẻ (tài liệu không chương): {n['name'][:40]}")

    print("\nCây bài vừa compile:")
    walk(roots)

    if args.flat_id:
        print(f"\n[3] Bài phẳng {args.flat_id} (chỉ có Điều): mọi node là root, không nest...")
        code, body = get(args.base, token, f"/articles/{args.flat_id}/tree")
        tree2 = body.get("data") or {}
        roots2 = tree2.get("roots") or []
        check(code == 200 and roots2, f"tree bài {args.flat_id} trả 200 có roots")
        check(all(not (r.get("children") or []) for r in roots2),
              "cây phẳng: không root nào có con")
        check(tree2.get("node_count") == len(roots2),
              f"node_count={tree2.get('node_count')} == số root={len(roots2)}")

    print("\n" + ("E2E OK — 0 failure" if not fails else f"E2E FAIL — {len(fails)} failure:"))
    for f in fails:
        print("  x " + f)
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
