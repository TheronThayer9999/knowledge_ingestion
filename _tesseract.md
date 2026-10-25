# Tesseract OCR - Tiếng Việt

## Thư viện cần thiết

```
pytesseract>=0.3.13
Pillow>=10.2.0
```

## Cài đặt

### 1. Cài Tesseract Engine (Windows)

```bash
winget install UB-Mannheim.TesseractOCR
```

Sau khi cài, thêm vào PATH hoặc chỉ định đường dẫn trong code:

```python
pytesseract.pytesseract.tesseract_cmd = r'C:\Program Files\Tesseract-OCR\tesseract.exe'
```

### 2. Download model tiếng Việt

```bash
# Download vie.traineddata
Invoke-WebRequest -Uri "https://github.com/tesseract-ocr/tessdata/raw/main/vie.traineddata" -OutFile "vie.traineddata"

# Copy vào thư mục tessdata (cần quyền admin)
Copy-Item "vie.traineddata" "C:\Program Files\Tesseract-OCR\tessdata\vie.traineddata" -Force
```

### 3. Cài Python package

```bash
pip install pytesseract Pillow
```

Hoặc với uv:

```bash
uv pip install pytesseract Pillow
```

## Code mẫu

```python
import pytesseract
from PIL import Image

pytesseract.pytesseract.tesseract_cmd = r'C:\Program Files\Tesseract-OCR\tesseract.exe'

img = Image.open('anh.png')
text = pytesseract.image_to_string(img, lang='vie')
print(text)
```

## Danh sách ngôn ngữ có sẵn

```bash
tesseract --list-langs
```

Output:

```
eng
osd
vie
```

## Đa ngôn ngữ — `vie+eng`

Máy đã cài **cả** `vie` và `eng` (kèm `osd`). Tesseract nhận nhiều model qua dấu `+`, engine
chọn model tốt hơn cho **từng từ**:

```bash
tesseract anh.png stdout -l vie+eng --psm 6
```

- Dùng khi: văn bản Việt có thuật ngữ/tên riêng tiếng Anh, phụ lục song ngữ, hoá đơn.
- Đừng bật khi chỉ có tiếng Việt: nạp 2 model ⇒ chậm hơn ~1.5–2×/trang.
- Repo khai đúng chuỗi này ở `ocr.lang` (`configs/config.json`). Lúc boot worker gọi
  `tesseract --list-langs`; thiếu gói nào thì **warn** kèm danh sách gói đang có
  (`extractor.MissingLangs`), vì thiếu traineddata làm tesseract exit `!= 0` cho **mọi** trang
  chứ không phải chỉ đọc kém.

## Giới hạn: chữ viết tay

Tesseract học trên **chữ in**. Chữ in — Việt hay Anh — đọc tốt; còn **chữ viết tay, chữ ký,
ghi chú tay** gần như không đọc được, chỉnh `--psm` hay thêm model cũng không cứu.

⇒ Đó là ca **duy nhất** đáng gọi VLM (qwen-vl). Không gọi VLM vì "tài liệu tiếng Anh" hay
"trang có bảng" — hai ca đó đi đường tất định (`tesseract` + `pdftohtml -xml`).

## Đánh giá

| Tiêu chí | Đánh giá |
|----------|----------|
| 1 dòng | ✅ Hoàn hảo |
| Nhiều dòng | ✅ Gần hoàn hảo |
| Dấu tiếng Việt | ✅ Tốt nhất |
| Chữ in tiếng Anh (`vie+eng`) | ✅ Tốt |
| Chữ viết tay | ❌ Không đọc được (ca cần VLM) |
| Tốc độ | ✅ Nhanh (`vie+eng` chậm hơn ~1.5–2×) |
| Cài đặt | ⚠️ Cần download model riêng |

## So sánh với các model khác

| Model | 1 dòng | Nhiều dòng | Dấu tiếng Việt |
|-------|--------|------------|----------------|
| VietOCR | ✅ Tốt | ❌ Sai hoàn toàn | ✅ Tốt |
| EasyOCR | ⚠️ Vỡ mảnh | ⚠️ Đọc được | ❌ Lỗi dấu nặng |
| PaddleOCR | ❌ Không dấu | ❌ Không dấu | ❌ Không có |
| **Tesseract** | ✅ **Hoàn hảo** | ✅ **Gần hoàn hảo** | ✅ **Tốt nhất** |

## Kết luận

**Tesseract là lựa chọn tốt nhất cho OCR chữ in** (Việt + Anh) — đọc được cả 1 dòng và nhiều
dòng, giữ dấu chính xác nhất. Chữ viết tay là ngoại lệ duy nhất, và đó là lúc dùng VLM.
