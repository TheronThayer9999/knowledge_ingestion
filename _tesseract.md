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

## Đánh giá

| Tiêu chí | Đánh giá |
|----------|----------|
| 1 dòng | ✅ Hoàn hảo |
| Nhiều dòng | ✅ Gần hoàn hảo |
| Dấu tiếng Việt | ✅ Tốt nhất |
| Tốc độ | ✅ Nhanh |
| Cài đặt | ⚠️ Cần download model riêng |

## So sánh với các model khác

| Model | 1 dòng | Nhiều dòng | Dấu tiếng Việt |
|-------|--------|------------|----------------|
| VietOCR | ✅ Tốt | ❌ Sai hoàn toàn | ✅ Tốt |
| EasyOCR | ⚠️ Vỡ mảnh | ⚠️ Đọc được | ❌ Lỗi dấu nặng |
| PaddleOCR | ❌ Không dấu | ❌ Không dấu | ❌ Không có |
| **Tesseract** | ✅ **Hoàn hảo** | ✅ **Gần hoàn hảo** | ✅ **Tốt nhất** |

## Kết luận

**Tesseract là lựa chọn tốt nhất cho OCR tiếng Việt** — đọc được cả 1 dòng và nhiều dòng, giữ dấu chính xác nhất.
