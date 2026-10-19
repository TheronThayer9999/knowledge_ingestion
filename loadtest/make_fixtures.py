"""Sinh bộ file thật để test pipeline end-to-end.

Mục tiêu: mỗi file nhắm 1 nhánh khác nhau của extractor, không dùng file giả
chung 1 khuôn — nếu không sẽ không phát hiện được lỗi chỉ xảy ra ở nhánh đó.

  1. txt_noi_dung.txt   — 1 trang, ~500k từ. Bài lớn nhất mà ta dám tạo trên
                         máy này: đo trần RAM của phase index.
  2. txt_nhieu_dieu.txt — ~120 Điều, nhiều trang logic để tách block.
  3. pdf_text_layer.pdf — PDF có text layer thật (KHÔNG OCR). Kiểm tra đường
                         "đa số PDF không phải ảnh, đừng OCR rộng".
  4. pdf_scan.pdf       — PDF chỉ có ảnh, KHÔNG có text layer. Bắt buộc OCR.
  5. docx_viet.docx     — docx có bảng + heading.
  6. rong.txt           — file rỗng. Edge case: phải báo lỗi, không đánh done giả.

PDF viết tay bằng thư viện pypdf/reportlab vì không có LibreOffice trên máy;
pdf_scan thì nhúng ảnh PNG render từ chính text bằng Pillow để Tesseract đọc
được (nếu nhúng ảnh toàn trắng thì OCR không ra chữ và test vô nghĩa).
"""

import os
import sys
import zipfile
from pathlib import Path

OUT = Path(__file__).resolve().parent / "fixtures"
OUT.mkdir(exist_ok=True)


# ---------- nội dung tiếng Việt có cấu trúc Điều/mục ----------

def build_regulations(n_dieu: int, items_per_dieu: int = 4) -> str:
    """Sinh văn bản nhiều Điều, mỗi Điều có mục đánh số."""
    chu_de = [
        "làm việc từ xa", "chế độ nghỉ ốm", "thưởng dự án", "phép năm",
        "đóng bảo hiểm", "chính sách ăn trưa", "nghỉ việc", "kỷ luật",
        "đào tạo", "sức khỏe", "bảo mật thông tin", "mua sắm",
        "công tác nước ngoài", "làm thêm giờ", "thưởng sáng kiến",
    ]
    lines = ["QUY CHẾ NHÂN SỰ — TÀI LIỆU MẪU", "Phiên bản: 2024-Q3",
             "Bộ phận ban hành: Phòng Nhân sự (HR)", ""]
    for d in range(1, n_dieu + 1):
        lines.append(f"ĐIỀU {d}: QUY ĐỊNH VỀ {chu_de[(d - 1) % len(chu_de)].upper()}")
        for i in range(1, items_per_dieu + 1):
            lines.append(
                f"{i}. Nhân viên chính thức làm việc theo quy định {i} về "
                f"{chu_de[(d - 1) % len(chu_de)]}, áp dụng từ ngày ban hành. "
                f"Trường hợp vi phạm sẽ bị nhắc nhở và xử lý theo mức độ."
            )
        lines.append("")
    return "\n".join(lines)


def write_text_files():
    # Bài rỗng — edge case extractor phải từ chối, không đánh done giả.
    (OUT / "rong.txt").write_text("", encoding="utf-8")

    # ~120 Điều.
    (OUT / "txt_nhieu_dieu.txt").write_text(
        build_regulations(120), encoding="utf-8")

    # Bài lớn: đo trần RAM. 500k "từ" ~ 3.5MB text.
    body = build_regulations(4000, items_per_dieu=8)
    (OUT / "txt_noi_dung.txt").write_text(body, encoding="utf-8")
    return len(body)


# ---------- PDF ----------

def write_pdfs():
    try:
        from reportlab.lib.pagesizes import A4
        from reportlab.pdfgen import canvas
    except ImportError:
        print("skip pdf: pip install reportlab", file=sys.stderr)
        return None

    # 1) PDF có text layer thật — KHÔNG được OCR.
    text = build_regulations(30)
    c = canvas.Canvas(str(OUT / "pdf_text_layer.pdf"), pagesize=A4)
    per_page = 40
    ls = text.split("\n")
    for start in range(0, len(ls), per_page):
        y = 800
        for line in ls[start:start + per_page]:
            c.drawString(56, y, line[:95])
            y -= 14
        c.showPage()
    c.save()

    # 2) PDF scan: render text ra PNG rồi nhúng ảnh, KHÔNG để lại text layer.
    #    Dùng Pillow vì cần font hỗ trợ dấu tiếng Việt.
    try:
        from PIL import Image, ImageDraw, ImageFont
    except ImportError:
        print("skip pdf_scan: pip install pillow", file=sys.stderr)
        return None

    font = _load_viet_font(size=26)
    scan_text = build_regulations(6)
    ls = [l for l in scan_text.split("\n") if l.strip()]
    per_img = 22
    imgs = []
    for start in range(0, len(ls), per_img):
        img = Image.new("RGB", (1240, 1754), "white")  # A4 @150dpi
        d = ImageDraw.Draw(img)
        y = 120
        for line in ls[start:start + per_img]:
            d.text((90, y), line[:70], fill="black", font=font)
            y += 60
        p = OUT / f"_page{start // per_img}.png"
        img.save(p)
        imgs.append(p)

    c = canvas.Canvas(str(OUT / "pdf_scan.pdf"), pagesize=A4)
    for p in imgs:
        c.drawImage(str(p), 0, 0, width=A4[0], height=A4[1])
        c.showPage()
    c.save()
    for p in imgs:
        p.unlink()
    return len(imgs)


def _load_viet_font(size: int):
    """Tìm font có dấu tiếng Việt, không có thì dùng default (sẽ ra ô vuông
    và OCR vô nghĩa — nên in cảnh báo để không âm thầm test sai)."""
    from PIL import ImageFont
    cands = [
        r"C:\Windows\Fonts\arial.ttf",      # có đủ dấu tiếng Việt
        r"C:\Windows\Fonts\segoeui.ttf",
        r"C:\Windows\Fonts\tahoma.ttf",
    ]
    for path in cands:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    print("WARNING: không tìm thấy font hỗ trợ tiếng Việt", file=sys.stderr)
    return ImageFont.load_default()


# ---------- DOCX ----------

def write_docx():
    """Viết docx tay bằng zipfile — không cần python-docx."""
    try:
        from docx import Document
    except ImportError:
        print("skip docx: pip install python-docx", file=sys.stderr)
        return None
    doc = Document()
    doc.add_heading("QUY TRÌNH VẬN HÀNH", level=1)
    doc.add_paragraph("Tài liệu mô tả quy trình vận hành hằng ngày của bộ phận.")
    for d in range(1, 13):
        doc.add_heading(f"Mục {d}: Bước thực hiện {d}", level=2)
        doc.add_paragraph(
            f"Bước {d} yêu cầu người thực hiện kiểm tra đầu vào, thực hiện theo "
            f"trình tự, và ghi nhận kết quả vào biểu mẫu 04A.")
    doc.add_paragraph(
        "Bảng theo dõi:")
    table = doc.add_table(rows=1, cols=3)
    hdr = table.rows[0].cells
    for i, h in enumerate(["Bước", "Người thực hiện", "Thời hạn"]):
        hdr[i].text = h
    for r in range(1, 6):
        cells = table.add_row().cells
        cells[0].text = str(r)
        cells[1].text = "Nhân viên phụ trách"
        cells[2].text = "Cuối ngày"
    doc.save(str(OUT / "docx_viet.docx"))
    return 1


if __name__ == "__main__":
    chars = write_text_files()
    pages = write_pdfs()
    docx = write_docx()
    print("fixtures:")
    for f in sorted(OUT.iterdir()):
        print(f"  {f.name:24s} {f.stat().st_size:>10,d} bytes")
    print(f"txt_noi_dung: {chars:,} chars")
    print(f"pdf_scan pages: {pages}  docx: {docx}")