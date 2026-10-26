r"""Sinh data PHỨC TẠP để test chất lượng câu trả lời + tool Excel (không phải đo tải).

Vì sao cần data riêng: bộ fixture cũ trong `loadtest/fixtures` chỉ chứng minh
extractor CHẠY được (1 trang text, 1 PDF scan...). Data đó không đủ để ép model
phải chọn sheet, đối chiếu hai file xung đột, hay suy luận qua 2 tầng tham chiếu
(Điều -> Phụ lục). Muốn biết model trả lời "như chuyên gia" hay không thì dữ
liệu phải có bẫy thật.

Không có openpyxl trên máy (đã kiểm tra `python -c "import openpyxl"`), nên
.xlsx viết tay bằng stdlib: `zipfile` + XML thô, chuỗi dùng `inlineStr` để khỏi
phải sinh sharedStrings. PDF dùng reportlab + font TrueType có dấu tiếng Việt
(Helvetica mặc định không render được dấu, ra ô vuông -> test vô nghĩa).

File sinh ra (đặt tại `loadtest/fixtures_complex/`):

  bang_luong_q3.xlsx            3 sheet có dữ liệu + 1 sheet TRỐNG, tiêu đề 2
                                dòng, số thập phân, ô trống, %  ->  ép
                                list_sheets/read_sheet chọn đúng sheet, hỏi gộp
                                phải đọc >= 2 sheet.
  hop_dong_thue_mat.xlsx        cùng tên cột, cùng MÃ hợp đồng ở 2 sheet, số
                                khác nhau  ->  không được cộng nhầm sheet.
  quy_che_phi_v1.xlsx / _v2.xlsx  cùng dịch vụ, mức khác nhau, v2 ghi rõ bãi bỏ
                                v1  ->  xử lý xung đột/bản mới (prompt có luật,
                                chưa từng test bằng dữ liệu).
  bang_ke_toan_multi_sheet.xlsx   bảng dài có dòng Cộng + sheet đối chiếu ngân
                                hàng lệch 1 chứng từ  ->  trích dẫn phải trỏ
                                sheet+dòng, số dư phải tính chứ không đoán.
  quy_dinh_nhieu_phu_luc.pdf      Điều dẫn chiếu Phụ lục A/B, con số chỉ nằm ở
                                phụ lục  ->  chuỗi suy luận nhiều tầng.
  scan_vua_text_vua_anh.pdf       trang 1 text layer, trang 2 chỉ ảnh  ->  nhánh
                                OCR chạy cùng text thật.

Mỗi file tự kiểm chứng sau khi ghi: mở lại bằng `zipfile`, đếm sheet trong
`xl/workbook.xml`, đọc `sheetData` từng sheet, và in ra đúng text mà
`extractor.extractXlsx` sẽ sinh theo hợp đồng đã định trong `xlsx.go`
("## Sheet: <tên>" + mỗi hàng một dòng pipe, ô trống giữ chỗ theo toạ độ cột).
Phòng khi hợp đồng đó đổi, `verify_xlsx.py`/test Go sẽ bắt được — preview ở đây
chỉ để người đọc biết model sắp thấy gì, không phải bằng chứng extractor đúng.

Chạy:
  python make_complex_fixtures.py            # sinh + verify + in bảng
  python make_complex_fixtures.py --no-pdf   # chỉ Excel (máy không có reportlab)
"""

import argparse
import re
import sys
import zipfile
from pathlib import Path
from xml.sax.saxutils import escape

HERE = Path(__file__).resolve().parent
OUT = HERE / "fixtures_complex"

# ---------------------------------------------------------------- xlsx writer

CELL_NS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
REL_DOC = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
REL_WS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"


def col_ref(idx: int) -> str:
    """Cột 1-based -> chữ Excel (1=A, 26=Z, 27=AA)."""
    letters = ""
    while idx > 0:
        idx, rem = divmod(idx - 1, 26)
        letters = chr(ord("A") + rem) + letters
    return letters


def cell_xml(col: int, row: int, value) -> str:
    """Một ô. value None/"" -> KHÔNG sinh tag (Excel cũng bỏ ô rỗng khỏi XML,
    nên đây là cách tái hiện đúng tình huống mà xlsx.go phải xử lý: toạ độ ô
    mới là thứ giữ vị trí cột, không phải thứ tự xuất hiện)."""
    if value is None or value == "":
        return ""
    ref = f"{col_ref(col)}{row}"
    if isinstance(value, bool):
        value = str(value)
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        # Số nguyên vẹn viết là 113 chứ không phải 113.0 — Excel cũng lưu vậy,
        # và model đọc "113.0 ngày công" là cách người thật không nói.
        if isinstance(value, float) and value.is_integer():
            value = int(value)
        return f'<c r="{ref}"><v>{value}</v></c>'
    text = escape(str(value)).replace("\n", " ")
    return f'<c r="{ref}" t="inlineStr"><is><t>{text}</t></is></c>'


def sheet_xml(rows) -> str:
    cells = []
    for r, row in enumerate(rows, start=1):
        body = "".join(cell_xml(c, r, v) for c, v in enumerate(row, start=1))
        if body:
            cells.append(f'<row r="{r}">{body}</row>')
    return (f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            f'<worksheet xmlns="{CELL_NS}"><sheetData>'
            + "".join(cells) + "</sheetData></worksheet>")


def _content_types(n_sheets: int) -> str:
    ov = "".join(f'<Override PartName="/xl/worksheets/sheet{i}.xml" ContentType="application/'
                 f'vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>'
                 for i in range(1, n_sheets + 1))
    return ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
            '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
            '<Default Extension="xml" ContentType="application/xml"/>'
            '<Override PartName="/xl/workbook.xml" ContentType="application/'
            'vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>'
            + ov + "</Types>")


def _workbook_xml(names) -> str:
    sheets = "".join(f'<sheet name="{escape(n)}" sheetId="{i}" r:id="rId{i}"/>'
                     for i, n in enumerate(names, start=1))
    return ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            f'<workbook xmlns="{CELL_NS}" '
            'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">'
            f"<sheets>{sheets}</sheets></workbook>")


def _workbook_rels(n_sheets: int) -> str:
    rels = "".join(f'<Relationship Id="rId{i}" Type="{REL_WS}" Target="worksheets/sheet{i}.xml"/>'
                   for i in range(1, n_sheets + 1))
    return ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
            + rels + "</Relationships>")


def write_xlsx(path: Path, sheets) -> None:
    """sheets = [(tên sheet, hàng)] — hàng là list giá trị, None/"" = ô trống."""
    names = [name for name, _ in sheets]
    root_rels = ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
                 '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
                 f'<Relationship Id="rId1" Type="{REL_DOC}" Target="xl/workbook.xml"/>'
                 "</Relationships>")
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("[Content_Types].xml", _content_types(len(sheets)))
        zf.writestr("_rels/.rels", root_rels)
        zf.writestr("xl/workbook.xml", _workbook_xml(names))
        zf.writestr("xl/_rels/workbook.xml.rels", _workbook_rels(len(sheets)))
        for i, (_, rows) in enumerate(sheets, start=1):
            zf.writestr(f"xl/worksheets/sheet{i}.xml", sheet_xml(rows))


# --------------------------------------------------------------- self-verify

SHEET_HEADER_RE = re.compile(r'<sheet name="([^"]+)"')
CELL_RE = re.compile(r'<c r="([A-Z]+)\d+"(?: t="(\w+)")?[^>]*>'
                     r"(?:<v>(.*?)</v>|<is><t>(.*?)</t></is>)?</c>")
ROW_RE = re.compile(r"<row [^>]*>(.*?)</row>", re.S)


def _col_index(letters: str) -> int:
    n = 0
    for ch in letters:
        n = n * 26 + (ord(ch) - ord("A") + 1)
    return n


def read_back(path: Path):
    """Đọc lại file vừa sinh: [(tên sheet, [hàng đã điền chỗ trống theo cột])].
    Cùng luật với xlsx.go: ô trống giữ chỗ, hàng toàn ô trống bị bỏ."""
    with zipfile.ZipFile(path) as zf:
        names = SHEET_HEADER_RE.findall(zf.read("xl/workbook.xml").decode("utf-8"))
        out = []
        for i in range(1, len(names) + 1):
            xml = zf.read(f"xl/worksheets/sheet{i}.xml").decode("utf-8")
            rows = []
            for body in ROW_RE.findall(xml):
                cells = []
                for letters, kind, v, inline in CELL_RE.findall(body):
                    val = (v or inline or "").replace("&amp;", "&").replace("&lt;", "<")
                    pos = _col_index(letters)
                    while len(cells) < pos - 1:
                        cells.append("")
                    if len(cells) < pos:
                        cells.append(val)
                    else:
                        cells[pos - 1] = val
                while cells and cells[-1] == "":
                    cells.pop()
                if cells:
                    rows.append(cells)
            out.append((names[i - 1], rows))
        return out


def expected_extract(path: Path) -> str:
    """Text mà extractor sẽ sinh ra theo hợp đồng trong xlsx.go — dùng để in
    preview cho người đọc biết model sắp thấy gì. Sheet không có hàng nào bị
    BỎ HAI (xlsx.go: `if len(rows) == 0 { continue }`)."""
    blocks = []
    for name, rows in read_back(path):
        if not rows:
            continue
        head = f"## Sheet: {name}"
        lines = [head] + ["| " + " | ".join(r) + " |" for r in rows]
        blocks.append("\n".join(lines))
    return "\n\n".join(blocks)


def verify_xlsx(path: Path, want_sheets, want_rows: dict) -> list[str]:
    """So cấu trúc file với ý đồ thiết kế; trả danh sách lỗi (rỗng = đạt)."""
    errs = []
    got = read_back(path)
    names = [n for n, _ in got]
    if names != want_sheets:
        errs.append(f"sheets = {names}, mong đợi {want_sheets}")
    for name, rows in got:
        if name in want_rows and len(rows) != want_rows[name]:
            errs.append(f"sheet {name}: {len(rows)} hàng, mong đợi {want_rows[name]}")
    return errs


# ------------------------------------------------------------------- datasets

def bang_luong_q3() -> Path:
    """3 sheet có dữ liệu + 1 sheet trắng; tiêu đề 2 dòng; số thập phân, %."""
    luong = [
        ["BẢNG LƯƠNG QUÝ 3/2025 - KHỐI VĂN PHÒNG", None, None, None, None,
         None, None, None, None, None],
        ["Mã NV", "Họ và tên", "Chức vụ", "Ngày công", "Lương ngày (VNĐ)",
         "Lương cơ bản (VNĐ)", "Phụ cấp (VNĐ)", "Tổng thực nhận (VNĐ)",
         "Thuế TNCN (VNĐ)", "Nhận ròng (VNĐ)"],
    ]
    people = [
        ("NV01", "Nguyễn Thị Lan", "Chuyên viên", 22.5, 200000, 800000, 0.1),
        ("NV02", "Trần Văn Hưng", "Tổ trưởng", 24, 250000, 1200000, 0.1),
        ("NV03", "Lê Minh Ngọc", "Chuyên viên", 21.5, 180000, 0, 0.05),
        ("NV04", "Phạm Gia Bảo", "Tổ trưởng", 25, 320000, 2000000, 0.1),
        ("NV05", "Hoàng Thu Trang", "Nhân viên", 20, 150000, 600000, 0.05),
    ]
    tong = {"lc": 0, "pc": 0, "tt": 0, "thue": 0, "rong": 0, "cong": 0.0}
    for ma, ten, cv, ngay, luong_ngay, phu_cap, thue_rate in people:
        lc = ngay * luong_ngay
        tt = lc + phu_cap
        thue = round(tt * thue_rate)
        tong["lc"] += lc
        tong["pc"] += phu_cap
        tong["tt"] += tt
        tong["thue"] += thue
        tong["rong"] += tt - thue
        tong["cong"] += ngay
        luong.append([ma, ten, cv, ngay, luong_ngay, int(lc),
                      int(phu_cap) if phu_cap else None, int(tt), int(thue),
                      int(tt - thue)])
    luong.append(["Tổng cộng", None, None, tong["cong"], None, int(tong["lc"]),
                  int(tong["pc"]), int(tong["tt"]), int(tong["thue"]), int(tong["rong"])])

    phu_cap = [
        ["Mã NV", "Khoản phụ cấp", "Mức (VNĐ/tháng)", "Điều kiện hưởng", "Ghi chú"],
        ["NV01", "Phụ cấp ăn trưa", 800000,
         "Làm việc tại văn phòng từ 22 ngày công trở lên", None],
        ["NV02", "Phụ cấp độc hại", 1200000, "Làm việc trực tiếp tại xưởng",
         "Áp dụng từ 01/07/2025"],
        ["NV04", "Phụ cấp trách nhiệm", 2000000, "Tổ trưởng tổ sản xuất", None],
        ["NV05", "Phụ cấp ăn trưa", 600000,
         "Làm việc tại văn phòng dưới 22 ngày công", "NV05 nghỉ 5 ngày trong quý"],
        ["NV03", "Phụ cấp ăn trưa", 600000,
         "Làm việc tại văn phòng dưới 22 ngày công", "NV03 nghỉ 4 ngày trong quý"],
    ]
    ghi_chu = [
        ["Nội quy kỳ lương", "Giá trị"],
        ["Kỳ tính lương", "Quý 3/2025 (01/07/2025 - 30/09/2025)"],
        ["Tỷ lệ thuế TNCN bậc 1", "10%"],
        ["Tỷ lệ thuế TNCN bậc thấp", "5%"],
        ["Ngày lễ, Tết đi làm", "Tính hệ số 300% lương ngày"],
        ["Hạn chốt lương", "Ngày 05 của tháng kế tiếp"],
        ["Người lập bảng", "Phòng Nhân sự - cập nhật ngày 05/10/2025"],
    ]
    # Sheet trắng: xlsx.go bỏ sheet không có hàng nào -> list_sheets chỉ thấy 3.
    p = OUT / "bang_luong_q3.xlsx"
    write_xlsx(p, [("Luong", luong), ("Phu_cap", phu_cap), ("Ghi_chu", ghi_chu),
                   ("So_lieu_cho_cap_nhat", [])])
    return p


def hop_dong_thue_mat() -> Path:
    """Cùng tên cột, cùng mã HM-01 ở 2 sheet, mức tiền khác nhau."""
    head = ["Mã hợp đồng", "Diện tích (m2)", "Đơn giá (VNĐ/m2/tháng)",
            "Phí dịch vụ (VNĐ/m2/tháng)", "Thành tiền (VNĐ/tháng)"]

    def block(rows):
        out = [head[:]]
        for ma, dien_tich, don_gia, phi in rows:
            out.append([ma, dien_tich, don_gia, phi,
                        dien_tich * (don_gia + phi)])
        return out

    van_phu = block([("HM-01", 250, 180000, 15000), ("HM-02", 120, 200000, 15000)])
    kho = block([("HM-01", 1200, 60000, 8000), ("HM-03", 800, 55000, 8000)])
    ghi_chu = [
        ["Quy ước", "Thành tiền = Diện tích x (Đơn giá + Phí dịch vụ)"],
        ["Lưu ý", "Mã HM-01 là cùng một khách hàng nhưng hai mặt bằng khác nhau"],
        ["Kỳ thanh toán", "Ngày 10 hằng tháng, chuyển khoản"],
    ]
    p = OUT / "hop_dong_thue_mat.xlsx"
    write_xlsx(p, [("Thue_Van_Phu", van_phu), ("Thue_Kho", kho), ("Ghi_chu", ghi_chu)])
    return p


def quy_che_phi(version: int) -> Path:
    """v1 và v2 cùng dịch vụ nhưng mức khác nhau; v2 ghi rõ bãi bỏ v1."""
    v1_rows = [("PQ-01", "Cấp bản sao văn bản", 5000, "Bản/lần"),
               ("PQ-02", "Công chứng hợp đồng", 100000, "Hợp đồng"),
               ("PQ-03", "Phí tra cứu hồ sơ", 30000, "Lần tra cứu")]
    v2_rows = [("PQ-01", "Cấp bản sao văn bản", 8000, "Bản/lần"),
               ("PQ-02", "Công chứng hợp đồng", 150000, "Hợp đồng"),
               ("PQ-03", "Phí tra cứu hồ sơ", 30000, "Lần tra cứu"),
               ("PQ-04", "Chứng thực chữ ký", 12000, "Chữ ký")]
    rows = v1_rows if version == 1 else v2_rows
    bieu_phi = [["Mã", "Dịch vụ", "Mức thu (VNĐ)", "Đơn vị tính"]] + [list(r) for r in rows]
    hieu_luc = [
        ["Phiên bản", f"{version}.0"],
        ["Ngày ban hành", "10/01/2024" if version == 1 else "05/09/2025"],
        ["Ngày áp dụng", "01/02/2024" if version == 1 else "01/10/2025"],
    ]
    if version == 2:
        hieu_luc.append(["Hiệu lực thay thế",
                         "Bãi bỏ toàn bộ quy chế phiên bản 1 kể từ 01/10/2025, "
                         "bao gồm mức 5.000 VNĐ cho dịch vụ PQ-01"])
    else:
        hieu_luc.append(["Ghi chú", "Áp dụng cho tới khi có quy chế mới"])
    p = OUT / f"quy_che_phi_v{version}.xlsx"
    write_xlsx(p, [("Bieu_phi", bieu_phi), ("Hieu_luc", hieu_luc)])
    return p


def bang_ke_toan() -> Path:
    """Bảng dài có dòng Cộng + sheet đối chiếu ngân hàng lệch đúng 1 chứng từ."""
    head = ["Số chứng từ", "Ngày", "Diện giải", "Nợ (VNĐ)", "Có (VNĐ)"]
    entries = [
        ("CT-011", "03/10/2025", "Chi phí văn phòng phẩm", 4200000, 0),
        ("CT-012", "05/10/2025", "Thanh toán tiền điện tháng 9", 11500000, 0),
        ("CT-013", "07/10/2025", "Thu tiền dịch vụ khách hàng A", 0, 30000000),
        ("CT-014", "09/10/2025", "Chi phí công tác", 6800000, 0),
        ("CT-015", "12/10/2025", "Thanh toán tiền nước", 2300000, 0),
        ("CT-016", "15/10/2025", "Thu tiền dịch vụ khách hàng B", 0, 45600000),
        ("CT-017", "17/10/2025", "Chi phí thuê mặt bằng", 18000000, 0),
        ("CT-018", "20/10/2025", "Sửa chữa thiết bị", 5400000, 0),
        ("CT-019", "22/10/2025", "Thu tiền dịch vụ khách hàng C", 0, 27800000),
        ("CT-020", "25/10/2025", "Chi phí đào tạo", 9600000, 0),
        ("CT-021", "28/10/2025", "Lệ phí ngân hàng", 450000, 0),
    ]
    so_chi = [head] + [list(e) for e in entries]
    tong_no = sum(e[3] for e in entries)
    tong_co = sum(e[4] for e in entries)
    so_chi.append(["Cộng phát sinh", None, None, tong_no, tong_co])
    # Số dư đặt trong cột Diễn giải: nhét vào cột Nợ hoặc Có thì model đọc
    # nhầm là một nghiệp vụ phát sinh, còn đây là hiệu của hai cột.
    so_du = tong_co - tong_no
    so_chi.append(["Số dư cuối kỳ theo sổ", None,
                   f"Có trừ Nợ = {so_du} VNĐ", None, None])

    # Ngân hàng chưa ghi nhận CT-019 -> số dư ngân hàng lệch đúng 27.800.000.
    doi_chieu = [
        ["Chỉ tiêu", "Số tiền (VNĐ)"],
        ["Số dư cuối kỳ theo ngân hàng", so_du - 27800000],
        ["Số dư cuối kỳ theo sổ kế toán", so_du],
        ["Chênh lệch cần giải trình", 27800000],
        ["Nguyên nhân", "Chưa ghi nhận chứng từ CT-019 (thu khách hàng C)"],
    ]
    quy_uoc = [
        ["Quy ước", "Chứng từ ghi Nợ là chi tiền, ghi Có là thu tiền"],
        ["Kỳ đối chiếu", "Tháng 10/2025"],
        ["Người lập", "Phòng Kế toán - cập nhật ngày 31/10/2025"],
    ]
    p = OUT / "bang_ke_toan_multi_sheet.xlsx"
    write_xlsx(p, [("So_chi_ti_tai", so_chi), ("Doi_chieu_ngan_hang", doi_chieu),
                   ("Quy_uoc", quy_uoc)])
    return p


# ------------------------------------------------------------------ PDF data

PHU_LUC_PAGES = [
    [
        "QUY ĐỊNH VỀ CẤP BẢN SAO VÀ PHÍ TIỆN ÍCH",
        "Phiên bản 3.0 - ban hành ngày 05/9/2025 - có hiệu lực từ 01/10/2025",
        "",
        "ĐIỀU 1. Phạm vi điều chỉnh",
        "Quy định này áp dụng cho việc cấp bản sao văn bản, chứng thực chữ ký và",
        "phí tiện ích tại trụ sở chính.",
        "",
        "ĐIỀU 2. Mức phí cấp bản sao",
        "1. Mức phí cấp bản sao thực hiện theo Phụ lục A của quy định này.",
        "2. Bản sao từ trang thứ 11 trở đi tính theo điểm b Phụ lục A.",
        "3. Phí dịch thuật tài liệu theo điểm c Phụ lục A.",
        "",
        "ĐIỀU 3. Trường hợp miễn phí",
        "1. Các trường hợp miễn phí thực hiện theo Phụ lục B.",
        "2. Mỗi loại giấy tờ được cấp miễn phí không quá 02 bản.",
        "",
        "ĐIỀU 4. Thủ tục",
        "1. Người yêu cầu xuất trình bản chính và khai phiếu yêu cầu.",
        "2. Thời hạn trả kết quả là 03 ngày làm việc kể từ ngày nhận đủ hồ sơ.",
        "",
        "ĐIỀU 5. Hiệu lực thi hành",
        "Quy định này thay thế phiên bản 2.0 kể từ ngày 01/10/2025.",
    ],
    [
        "PHỤ LỤC A - BIỂU PHÍ CẤP BẢN SAO VÀ DỊCH THUẬT",
        "(Ban hành kèm Quy định phiên bản 3.0)",
        "",
        "a) Bản sao từ trang thứ 01 đến trang thứ 10: 3.000 VNĐ/trang.",
        "b) Phần bản sao từ trang thứ 11 trở đi: 2.000 VNĐ/trang.",
        "c) Dịch thuật tài liệu: 150.000 VNĐ/tài liệu, chưa gồm phí bản sao.",
        "d) Phí chuyển phát nhanh (nếu người yêu cầu đăng ký): 25.000 VNĐ/hồ sơ.",
        "",
        "Ví dụ tính phí bản sao 25 trang:",
        "10 trang đầu x 3.000 = 30.000 VNĐ",
        "15 trang còn lại x 2.000 = 30.000 VNĐ",
        "Tổng cộng: 60.000 VNĐ, chưa gồm phí dịch thuật và chuyển phát.",
    ],
    [
        "PHỤ LỤC B - TRƯỜNG HỢP MIỄN PHÍ",
        "",
        "1. Người có công với cách mạng, thân nhân liệt sĩ.",
        "2. Hồ sơ khiếu nại lần đầu của công dân.",
        "3. Bản sao phục vụ mục đích học tập của học sinh phổ thông.",
        "",
        "Không miễn phí đối với bản sao phục vụ giao dịch dân sự có giá trị",
        "từ 50.000.000 VNĐ trở lên.",
    ],
]


def _pdf_with_text_layer(path: Path, pages) -> None:
    from reportlab.lib.pagesizes import A4
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    from reportlab.pdfgen import canvas

    font = _register_viet_font()
    c = canvas.Canvas(str(path), pagesize=A4)
    width, height = A4
    for lines in pages:
        y = height - 70
        for line in lines:
            c.setFont(font, 12)
            c.drawString(56, y, line)
            y -= 18
        c.showPage()
    c.save()


def _register_viet_font() -> str:
    """Arial có đủ dấu tiếng Việt; không tìm thấy font thì báo rõ vì PDF không
    dấu sẽ làm OCR và truy xuất vô nghĩa."""
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont

    for cand in (r"C:\Windows\Fonts\arial.ttf", r"C:\Windows\Fonts\tahoma.ttf",
                 r"C:\Windows\Fonts\segoeui.ttf"):
        if Path(cand).exists():
            pdfmetrics.registerFont(TTFont("ViFont", cand))
            return "ViFont"
    print("WARNING: khong co font tieng Viet, PDF se khong dau", file=sys.stderr)
    return "Helvetica"


def quy_dinh_nhieu_phu_luc() -> Path:
    p = OUT / "quy_dinh_nhieu_phu_luc.pdf"
    _pdf_with_text_layer(p, PHU_LUC_PAGES)
    return p


def scan_vua_text_vua_anh() -> Path | None:
    """Trang 1 có text layer, trang 2 chỉ là ảnh -> buộc OCR chạy một phần."""
    try:
        from PIL import Image, ImageDraw
    except ImportError:
        print("skip scan_vua_text_vua_anh.pdf: pip install pillow", file=sys.stderr)
        return None
    from reportlab.lib.pagesizes import A4
    from reportlab.pdfgen import canvas

    text_lines = [
        "PHẦN TEXT LAYER - CHÍNH SÁCH LÀM VIỆC TỪ XA",
        "1. Nhân viên chính thức được làm việc từ xa tối đa 02 ngày mỗi tuần.",
        "2. Phải đăng ký trước với quản lý trực tiếp chậm nhất 01 ngày.",
        "3. Trường hợp dịch bệnh, được làm việc từ xa tối đa 14 ngày",
        "   mà không cần đăng ký trước.",
    ]
    img_lines = [
        "PHẦN ẢNH (OCR) - QUY ĐỊNH VỀ BẢO MẬT",
        "1. Cấm sao nhãng tài liệu mật ra ngoài tổ chức.",
        "2. Vi phạm bị xử lý kỷ luật ở mức cao nhất.",
        "3. Tài liệu mật phải lưu trong tủ có khóa.",
    ]
    png = OUT / "_scan_page.png"
    img = Image.new("RGB", (1240, 1754), "white")
    draw = ImageDraw.Draw(img)
    font = _load_pil_font(30)
    y = 140
    for line in img_lines:
        draw.text((100, y), line, fill="black", font=font)
        y += 62
    img.save(png)

    p = OUT / "scan_vua_text_vua_anh.pdf"
    c = canvas.Canvas(str(p), pagesize=A4)
    font_name = _register_viet_font()
    y = A4[1] - 70
    c.setFont(font_name, 12)
    for line in text_lines:
        c.drawString(56, y, line)
        y -= 20
    c.showPage()
    c.drawImage(str(png), 0, 0, width=A4[0], height=A4[1])
    c.showPage()
    c.save()
    png.unlink()
    return p


def _load_pil_font(size: int):
    from PIL import ImageFont

    for cand in (r"C:\Windows\Fonts\arial.ttf", r"C:\Windows\Fonts\tahoma.ttf"):
        if Path(cand).exists():
            return ImageFont.truetype(cand, size)
    return ImageFont.load_default()


# ---------------------------------------------------------------------- main

def main():
    global OUT
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=str(OUT))
    ap.add_argument("--no-pdf", action="store_true",
                    help="chỉ sinh Excel (máy không có reportlab/Pillow)")
    ap.add_argument("--preview", type=int, default=6,
                    help="số dòng text trích được in ra cho mỗi file Excel")
    args = ap.parse_args()

    OUT = Path(args.out)
    OUT.mkdir(parents=True, exist_ok=True)

    made, problems = [], []

    # (file, sheets mong đợi, số hàng mong đợi theo tên sheet)
    specs = {
        "bang_luong_q3.xlsx": (["Luong", "Phu_cap", "Ghi_chu", "So_lieu_cho_cap_nhat"],
                               {"Luong": 8, "Phu_cap": 6, "Ghi_chu": 7}),
        "hop_dong_thue_mat.xlsx": (["Thue_Van_Phu", "Thue_Kho", "Ghi_chu"],
                                   {"Thue_Van_Phu": 3, "Thue_Kho": 3, "Ghi_chu": 3}),
        "quy_che_phi_v1.xlsx": (["Bieu_phi", "Hieu_luc"],
                                {"Bieu_phi": 4, "Hieu_luc": 4}),
        "quy_che_phi_v2.xlsx": (["Bieu_phi", "Hieu_luc"],
                                {"Bieu_phi": 5, "Hieu_luc": 4}),
        "bang_ke_toan_multi_sheet.xlsx": (["So_chi_ti_tai", "Doi_chieu_ngan_hang", "Quy_uoc"],
                                          {"So_chi_ti_tai": 14, "Doi_chieu_ngan_hang": 5,
                                           "Quy_uoc": 3}),
    }

    builders = {
        "bang_luong_q3.xlsx": bang_luong_q3,
        "hop_dong_thue_mat.xlsx": hop_dong_thue_mat,
        "quy_che_phi_v1.xlsx": lambda: quy_che_phi(1),
        "quy_che_phi_v2.xlsx": lambda: quy_che_phi(2),
        "bang_ke_toan_multi_sheet.xlsx": bang_ke_toan,
    }
    for name, build in builders.items():
        path = build()
        errs = verify_xlsx(path, *specs[name])
        if errs:
            problems.extend(f"{name}: {e}" for e in errs)
        made.append(path)
        extract = expected_extract(path)
        print(f"\n=== {name} ({path.stat().st_size:,} bytes) ===")
        n_sheets = len([b for b in extract.split("\n\n") if b.startswith("## Sheet")])
        print(f"  extractor thấy {n_sheets} sheet "
              f"(khai báo {len(specs[name][0])} - sheet trống bị bỏ có chủ đích)")
        for line in extract.splitlines()[:args.preview]:
            print(f"  | {line[:120]}")

    if not args.no_pdf:
        try:
            made.append(quy_dinh_nhieu_phu_luc())
            p = scan_vua_text_vua_anh()
            if p:
                made.append(p)
        except ImportError as e:
            problems.append(f"bo qua PDF: thieu thu vien {e}")

    print("\n---- files ----")
    for f in sorted(OUT.iterdir()):
        print(f"  {f.name:32s} {f.stat().st_size:>10,d} bytes")
    if problems:
        print("\nLỖI KIỂM CHỨNG:")
        for p in problems:
            print(f"  ! {p}")
        sys.exit(1)
    print(f"\nOK: {len(made)} file, tự kiểm chứng đạt.")
    print("Upload thử: chọn các file này trong UI, hoặc chạy")
    print("  python expert_eval.py --docs " + " ".join(str(f) for f in made))


if __name__ == "__main__":
    main()
