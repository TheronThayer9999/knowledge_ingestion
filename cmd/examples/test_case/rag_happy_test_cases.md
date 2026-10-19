# Bộ Test Cases Kiểm Thử Hệ Thống RAG (Happy Cases)

Tài liệu này chứa các kịch bản kiểm thử thành công (Happy Cases) dành cho hệ thống RAG, sử dụng 3 file dữ liệu đầu vào:
1. `rag_test_data.txt` (Quy chuẩn kỹ thuật)
2. `hr_policy_data.txt` (Chính sách Nhân sự)
3. `incident_ops_guide.txt` (Vận hành & Sự cố)

---

## 1. Nhóm Test Case Truy Vấn Đơn Nguồn (Single-Document Retrieval)

> **Mục tiêu:** Kiểm tra khả năng định vị chính xác từng file tài liệu và trích xuất đúng thông tin mà không bị lẫn lộn bởi các dữ liệu khác.

### TC-01: Truy vấn kiến trúc Backend
- **File nguồn:** `rag_test_data.txt`
- **Prompt:** `Hệ thống ECS quy định sử dụng phiên bản .NET nào và dùng ORM gì?`
- **Expected Answer:** Sử dụng .NET 8 trở lên; Thư viện ORM chuẩn là Entity Framework Core (EF Core) kết hợp với Repository Pattern và Unit of Work.

### TC-02: Truy vấn quy định Frontend
- **File nguồn:** `rag_test_data.txt`
- **Prompt:** `Bộ công cụ UI và framework kiểm thử unit test chuẩn cho Angular là gì?`
- **Expected Answer:** Thiết kế UI sử dụng Tailwind CSS kết hợp Angular Material; Unit test bắt buộc viết bằng Jest.

### TC-03: Truy vấn chính sách WFH
- **File nguồn:** `hr_policy_data.txt`
- **Prompt:** `Nhân viên khối kỹ thuật được phép làm việc tại nhà (WFH) tối đa mấy ngày một tuần?`
- **Expected Answer:** Được phép WFH tối đa 02 ngày mỗi tuần (cần có sự đồng ý của Trưởng bộ phận/Manager).

### TC-04: Truy vấn thời gian nâng lương
- **File nguồn:** `hr_policy_data.txt`
- **Prompt:** `Việc xét duyệt tăng lương định kỳ diễn ra vào tháng mấy và tỷ lệ tăng thông thường là bao nhiêu?`
- **Expected Answer:** Thực hiện mỗi năm một lần vào tháng 3; Mức tăng thông thường dao động từ 8% đến 15% tùy theo Grade (A, B, C).

### TC-05: Truy vấn chỉ số SLA sự cố
- **File nguồn:** `incident_ops_guide.txt`
- **Prompt:** `Thời gian phản hồi (Response SLA) và thời gian khắc phục mục tiêu (MTTR) cho sự cố Sev-1 là bao lâu?`
- **Expected Answer:** Thời gian phản hồi (Response SLA): < 15 phút; Thời gian khắc phục mục tiêu (MTTR): < 2 giờ.

---

## 2. Nhóm Test Case Truy Vấn Đa Nguồn (Cross-Document Reasoning)

> **Mục tiêu:** Kiểm tra khả năng tổng hợp ngữ cảnh từ 2 hoặc cả 3 file dữ liệu cùng một lúc để đưa ra câu trả lời toàn diện.

### TC-06: Quy trình xử lý lỗi kết nối CSDL và phân cấp sự cố
- **File nguồn:** `rag_test_data.txt` & `incident_ops_guide.txt`
- **Prompt:** `Nếu dịch vụ .NET Backend gặp lỗi kết nối PostgreSQL do vượt quá connection pool, mã lỗi tương ứng là gì và cách xử lý nhanh như thế nào?`
- **Expected Answer:**
    - **Mã lỗi:** `ERR-101` (Database Connection Timeout).
    - **Cách xử lý:** Kiểm tra các Pod backend có bị leak connection không, sau đó thực hiện restart service bằng lệnh:
      ```bash
      kubectl rollout restart deployment/backend-api -n production
      ```

### TC-07: Tổng hợp các mốc thời gian và SLA quy trình nội bộ
- **File nguồn:** `hr_policy_data.txt` & `incident_ops_guide.txt`
- **Prompt:** `Liệt kê thời hạn (SLA) thực hiện các thủ tục sau: Nộp đơn xin nghỉ phép năm, nộp hồ sơ thanh toán công tác phí và thời gian tổ chức họp Post-mortem sau sự cố Sev-2.`
- **Expected Answer:**
    - **Nghỉ phép năm:** Nộp đơn trước ít nhất 03 ngày làm việc thông qua HR Portal.
    - **Công tác phí:** Nộp form đề nghị thanh toán trong vòng 05 ngày làm việc sau khi kết thúc chuyến đi.
    - **Họp Post-mortem:** Bắt buộc tổ chức trong vòng 48 giờ sau khi khắc phục xong sự cố.

### TC-08: Địa chỉ và kênh liên lạc các hệ thống nội bộ
- **File nguồn:** Tất cả 3 file (`rag_test_data.txt`, `hr_policy_data.txt`, `incident_ops_guide.txt`)
- **Prompt:** `Tổng hợp đường dẫn (URL) hoặc kênh liên lạc của các hệ thống nội bộ sau: Giám sát hiệu năng, Cổng thông tin nhân sự và Kênh hỗ trợ DevOps.`
- **Expected Answer:**
    - **Giám sát hiệu năng:** Grafana tại địa chỉ `https://grafana.internal.ecs.com`.
    - **Cổng thông tin nhân sự:** HR Portal (phân hệ Time & Attendance).
    - **Kênh hỗ trợ DevOps:** Kênh Slack `#devops-support` hoặc hotline nội bộ extension số `4040`.

---

## 3. Nhóm Test Case Trích Xuất Chi Tiết Kỹ Thuật & Code Snippets

> **Mục tiêu:** Kiểm tra xem LLM có giữ nguyên tính chính xác của tham số, câu lệnh shell, đường dẫn file hay không.

### TC-09: Trích xuất lệnh Shell / Đường dẫn Script
- **File nguồn:** `incident_ops_guide.txt`
- **Prompt:** `Cho tôi xin đường dẫn script để bật chế độ Maintenance Mode cho ứng dụng.`
- **Expected Answer:** Chạy script tự động tại đường dẫn `/scripts/ops/enable-maintenance.sh` trên máy chủ Jenkins (hoặc bật cờ `MaintenanceMode: True` trong Nginx Ingress Controller).

### TC-10: Trích xuất Cấu hình / Connection String
- **File nguồn:** `rag_test_data.txt`
- **Prompt:** `Định dạng chuỗi kết nối Redis cluster trong file appsettings.json của ứng dụng .NET được quy định như thế nào?`
- **Expected Answer:** Khai báo tại mục `"ConnectionStrings:Redis"` trong file `appsettings.json` với định dạng: `"redis-node-1:6379,redis-node-2:6379,abortConnect=false"`.

---

## 📊 Tiêu Chí Đánh Giá Chấm Điểm RAG (RAG Evaluation Metrics)

| Tiêu chí | Mô tả | Trạng thái đạt (Pass) |
| :--- | :--- | :--- |
| **Context Retrieval** | Vector DB tìm đúng đoạn chunk chứa đáp án trong 3 file. | Đúng chunk & file nguồn. |
| **Faithfulness** | LLM chỉ trả lời dựa trên thông tin được cấp, không bịa thêm (Hallucination). | 100% trung thực với dữ liệu. |
| **Code Integrity** | Các đoạn code, lệnh CLI, đường dẫn file, thông số giữ nguyên cấu trúc gốc. | Khớp từng ký tự/đường dẫn. |