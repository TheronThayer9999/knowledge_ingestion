# Đợt 1 — Đối chiếu tất định hai bản trích (X1)

## Mục tiêu

Có một hàm thuần (pure function) so sánh hai bản trích của **cùng một trang** — bản tất định
(text layer / L2 / OCR) và bản do VLM đọc — để biết chúng đồng ý, lệch nhẹ, hay xung đột.
Đây là điều kiện của mọi việc cross-check ở mục 6.18: không có nó thì không đo được
cross-check đang bắt lỗi hay chỉ đang đốt token.

## Phạm vi (làm trong đợt này)

- `src/common/extractor/compare.go` — `CompareExtraction(det, vlm string) CrossCheck`.
- `src/common/extractor/compare_test.go` — bảng test cho 10 ca.
- Quy tắc đã chốt trong `RAG_RESEARCH.md` §6.18-b:
  1. Chuẩn hoá NFC + gộp khoảng trắng + bỏ dòng trống.
  2. **Tập số phải bằng nhau tuyệt đối** — lệch 1 số là `conflict`, không cần đo similarity.
  3. Còn lại mới dùng word-level Jaccard: `>= 0.90` agree, `0.70–0.90` minor, `< 0.70` conflict.

## Ngoài phạm vi

- Adapter VLM (`src/infrastructure/vlm/ollama`) và section config `vlm` — đợt sau.
- Nối vào pipeline extract / reader agent — chưa.
- Không đổi hành vi hiện có của worker.

## Quyết định kỹ thuật

- Số được chuẩn hoá dấu nhóm nghìn (`1.000.000` = `1000000`, `0,05` = `0.05`) để nhiễu OCR
  về dấu phân cách không thành xung đột giả. Quy tắc: nhiều dấu cùng loại, hoặc một dấu có
  đúng 3 chữ số sau và 1–3 chữ số trước ⇒ dấu nhóm; còn lại là dấu thập phân.
- `%` được coi là **từ** riêng, không phải một phần của số — OCR hay tách rời `50 %`, nên
  `50%` và `50 %` phải bằng nhau, còn `50%` và `50` khác nhau ở phần từ.
- Giữ nguyên dấu tiếng Việt khi so từ: `má` và `ma` là hai từ khác nhau.
- Không dùng LLM để so chuỗi — đắt và không tái lập được.

## Nghiệm thu

- [ ] 10 ca test xanh, gồm: trùng hoàn toàn, chỉ khác khoảng trắng, khác dấu nhóm nghìn,
      khác dấu thập phân, lệch 1 số, một bên rỗng, cả hai rỗng, khác hoa/thường,
      `%` tách rời, bảng phẳng hoá vs markdown.
- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` đều xanh.

## Commit

1. `docs: bổ sung nghiên cứu RAG và kế hoạch đợt 1` — `RAG_RESEARCH.md`, file này.
2. `feat(extractor): đối chiếu tất định hai bản trích cùng trang` — `compare.go`, `compare_test.go`, `go.mod`, `go.sum`.
