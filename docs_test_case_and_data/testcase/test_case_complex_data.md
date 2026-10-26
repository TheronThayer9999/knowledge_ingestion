# Bộ câu test cho dữ liệu phức tạp (Đợt 25)

Chạy được ngay bằng `loadtest/expert_eval.py --file <file này>`. Mỗi case là một
lượt hỏi độc lập, đáp án mong đợi chứa **số thật trong fixture** ở
`loadtest/fixtures_complex/` (sinh bằng `make_complex_fixtures.py`) — lớp L1 của
bộ chấm dùng chính các số đó để đối chiếu, nên sửa fixture thì phải sửa đáp án.

Hành vi UI/DB và kịch bản demo: xem `chat_session_uiux_va_demo.md`.
Thang chấm hình thức: `expert_answer_rubric.md`.

## Nhóm A — bảng lương nhiều sheet, có sheet trống

### Case X-01
**Câu hỏi:** File bảng lương quý 3 có mấy sheet chứa dữ liệu, kể tên?
**Đáp án mong đợi:** 3 sheet có nội dung: Luong, Phu_cap, Ghi_chu. Sheet thứ tư khai trong workbook là So_lieu_cho_cap_nhat nhưng không có dòng nào nên không xuất hiện trong nội dung đọc được.
**Nguồn tham khảo:** bang_luong_q3.xlsx

### Case X-02
**Câu hỏi:** Tổng thực nhận của toàn bảng lương quý 3 là bao nhiêu?
**Đáp án mong đợi:** 29.970.000 VNĐ, lấy từ cột Tổng thực nhận của dòng Tổng cộng. Lưu ý tổng cột Nhận ròng là 27.346.500 VNĐ, không phải con số này.
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Luong, dòng Tổng cộng

### Case X-03
**Câu hỏi:** Vì sao NV05 chỉ được phụ cấp ăn trưa 600.000 trong khi NV01 được 800.000?
**Đáp án mong đợi:** Vì điều kiện hưởng khác nhau: phụ cấp ăn trưa 800.000/tháng áp dụng khi làm việc tại văn phòng từ 22 ngày công trở lên, dưới 22 ngày công thì 600.000/tháng. NV01 có 22,5 ngày công; NV05 chỉ 20 ngày công và nghỉ 5 ngày trong quý.
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Phu_cap + sheet Luong

### Case X-04
**Câu hỏi:** NV03 có được hưởng phụ cấp không, số liệu trong file có nhất quán không?
**Đáp án mong đợi:** Không nhất quán. Sheet Luong để trống ô Phụ cấp của NV03 (thực nhận 3.870.000 VNĐ đúng bằng lương cơ bản), trong khi sheet Phu_cap ghi NV03 được phụ cấp ăn trưa 600.000 VNĐ/tháng vì dưới 22 ngày công và nghỉ 4 ngày trong quý. Cần kế toán xác nhận, không tự chọn một trong hai.
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Luong + sheet Phu_cap

### Case X-05
**Câu hỏi:** Thuế TNCN trong bảng lương áp dụng tỷ lệ bao nhiêu phần trăm?
**Đáp án mong đợi:** Hai mức: 10% (bậc 1) và 5% (bậc thấp). Đối chiếu sheet Luong thì NV01, NV02, NV04 chịu 10%; NV03 và NV05 chịu 5%.
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Ghi_chu + sheet Luong

### Case X-06
**Câu hỏi:** Hạn chốt lương của kỳ này là ngày nào và bảng do ai lập?
**Đáp án mong đợi:** Hạn chốt lương là ngày 05 của tháng kế tiếp; bảng do Phòng Nhân sự lập, cập nhật ngày 05/10/2025. Kỳ tính lương là Quý 3/2025 từ 01/07/2025 đến 30/09/2025.
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Ghi_chu

### Case X-07
**Câu hỏi:** Ai nhận ròng cao nhất và nhận bao nhiêu?
**Đáp án mong đợi:** NV04 Phạm Gia Bảo, nhận ròng 9.000.000 VNĐ (thực nhận 10.000.000 trừ thuế 1.000.000).
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Luong

### Case X-08
**Câu hỏi:** Nếu NV02 đi làm ngày lễ thì được tính lương ngày lễ thế nào?
**Đáp án mong đợi:** Ngày lễ, Tết đi làm tính hệ số 300% lương ngày, nên với lương ngày 250.000 VNĐ thì một ngày lễ là 750.000 VNĐ. File không có số ngày lễ thực tế của NV02 nên không tính được tổng.
**Nguồn tham khảo:** bang_luong_q3.xlsx, sheet Ghi_chu + sheet Luong

## Nhóm B — hai sheet trùng tên cột và trùng mã hợp đồng

### Case X-09
**Câu hỏi:** Hợp đồng HM-01 một tháng hết bao nhiêu tiền?
**Đáp án mong đợi:** 130.350.000 VNĐ, gồm 48.750.000 VNĐ ở sheet Thuê Văn Phú và 81.600.000 VNĐ ở sheet Thuê Kho — cùng mã nhưng là hai mặt bằng khác nhau của một khách hàng.
**Nguồn tham khảo:** hop_dong_thue_mat.xlsx, sheet Thue_Van_Phu + Thue_Kho + Ghi_chu

### Case X-10
**Câu hỏi:** Riêng khách hàng thuê khối văn phòng, tổng tiền một tháng là bao nhiêu?
**Đáp án mong đợi:** 74.550.000 VNĐ (HM-01 48.750.000 + HM-02 25.800.000), chỉ tính sheet thuê văn phòng.
**Nguồn tham khảo:** hop_dong_thue_mat.xlsx, sheet Thue_Van_Phu

### Case X-11
**Câu hỏi:** Kiểm tra xem cột Thành tiền có đúng công thức không.
**Đáp án mong đợi:** Đúng cả 4 dòng theo quy ước Thành tiền = Diện tích x (Đơn giá + Phí dịch vụ): 250 m2 x 195.000 = 48.750.000; 120 m2 x 215.000 = 25.800.000; 1200 m2 x 68.000 = 81.600.000; 800 m2 x 63.000 = 50.400.000.
**Nguồn tham khảo:** hop_dong_thue_mat.xlsx, 2 sheet thuê + Ghi_chu

### Case X-12
**Câu hỏi:** Tiền thuê thanh toán vào lúc nào?
**Đáp án mong đợi:** Ngày 10 hằng tháng, hình thức chuyển khoản.
**Nguồn tham khảo:** hop_dong_thue_mat.xlsx, sheet Ghi_chu

## Nhóm C — hai phiên bản quy chế, bản mới bãi bỏ bản cũ

### Case X-13
**Câu hỏi:** Phí công chứng hợp đồng hiện hành là bao nhiêu?
**Đáp án mong đợi:** 150.000 VNĐ/hợp đồng theo quy chế phiên bản 2.0 áp dụng từ 01/10/2025; mức 100.000 VNĐ của phiên bản 1 đã bị bãi bỏ toàn bộ kể từ 01/10/2025.
**Nguồn tham khảo:** quy_che_phi_v2.xlsx, sheet Bieu_phi + Hieu_luc

### Case X-14
**Câu hỏi:** So với bản 1, bản 2 thay đổi những dịch vụ nào?
**Đáp án mong đợi:** Bản 2 tăng phí cấp bản sao từ 5.000 lên 8.000 VNĐ/bản/lần, tăng công chứng hợp đồng từ 100.000 lên 150.000 VNĐ, giữ nguyên tra cứu hồ sơ 30.000 VNĐ, và thêm dịch vụ mới PQ-04 chứng thực chữ ký 12.000 VNĐ/chữ ký.
**Nguồn tham khảo:** quy_che_phi_v1.xlsx + quy_che_phi_v2.xlsx, sheet Bieu_phi

### Case X-15
**Câu hỏi:** Cấp bản sao hết bao nhiêu tiền theo biểu phí?
**Đáp án mong đợi:** Theo biểu phí xlsx: 8.000 VNĐ cho mỗi bản/lần (mã PQ-01, bản 2), bản 1 là 5.000 VNĐ. Quy định cấp bản sao dạng PDF lại tính theo trang (3.000 VNĐ/trang), là đơn vị tính khác nhau nên không so sánh trực tiếp được.
**Nguồn tham khảo:** quy_che_phi_v2.xlsx + quy_dinh_nhieu_phu_luc.pdf

## Nhóm D — điều khoản dẫn sang phụ lục (multi-hop)

### Case X-16
**Câu hỏi:** Sao 25 trang tài liệu hết bao nhiêu tiền?
**Đáp án mong đợi:** 60.000 VNĐ: 10 trang đầu nhân 3.000 = 30.000, 15 trang còn lại nhân 2.000 = 30.000. Mức này nằm ở Phụ lục A, Điều 2 chỉ dẫn chiếu sang phụ lục.
**Nguồn tham khảo:** quy_dinh_nhieu_phu_luc.pdf, trang 2 (Phụ lục A)

### Case X-17
**Câu hỏi:** Vậy sao 30 trang thì bao nhiêu?
**Đáp án mong đợi:** 70.000 VNĐ: 10 trang đầu 3.000/trang = 30.000, 20 trang từ thứ 11 trở đi 2.000/trang = 40.000.
**Nguồn tham khảo:** quy_dinh_nhieu_phu_luc.pdf, trang 2 (Phụ lục A điểm a và b)

### Case X-18
**Câu hỏi:** Những ai được miễn phí và giới hạn bao nhiêu bản?
**Đáp án mong đợi:** Ba nhóm được miễn theo Phụ lục B: người có công với cách mạng và thân nhân liệt sĩ; hồ sơ khiếu nại lần đầu; bản sao phục vụ học tập của học sinh phổ thông. Mỗi loại giấy tờ cấp miễn phí không quá 02 bản. Không miễn phí cho bản sao phục vụ giao dịch dân sự có giá trị từ 50.000.000 VNĐ trở lên.
**Nguồn tham khảo:** quy_dinh_nhieu_phu_luc.pdf, Điều 3 + Phụ lục B (trang 1 và 3)

### Case X-19
**Câu hỏi:** Nộp hồ sơ bao lâu thì nhận được bản sao?
**Đáp án mong đợi:** 03 ngày làm việc kể từ ngày nhận đủ hồ sơ, người yêu cầu phải xuất trình bản chính và khai phiếu yêu cầu.
**Nguồn tham khảo:** quy_dinh_nhieu_phu_luc.pdf, Điều 4

### Case X-20
**Câu hỏi:** Có được cộng thêm phí dịch thuật và chuyển phát khi tính 60.000 VNĐ cho 25 trang không?
**Đáp án mong đợi:** Không tự động. 60.000 VNĐ chưa gồm phí dịch thuật (150.000 VNĐ/tài liệu) và phí chuyển phát nhanh 25.000 VNĐ/hồ sơ chỉ áp dụng nếu người yêu cầu đăng ký.
**Nguồn tham khảo:** quy_dinh_nhieu_phu_luc.pdf, trang 2 (Phụ lục A điểm c và d)

## Nhóm E — bảng kê dài có dòng Cộng và đối chiếu ngân hàng

### Case X-21
**Câu hỏi:** Tổng chi trong tháng 10 là bao nhiêu?
**Đáp án mong đợi:** 58.250.000 VNĐ ở cột Nợ, theo quy ước ghi Nợ là chi tiền, ghi Có là thu tiền.
**Nguồn tham khảo:** bang_ke_toan_multi_sheet.xlsx, sheet So_chi_ti_tai + Quy_uoc

### Case X-22
**Câu hỏi:** Cộng lại từng chứng từ chi rồi đối chiếu với dòng Cộng phát sinh, có khớp không?
**Đáp án mong đợi:** Khớp: 4.200.000 + 11.500.000 + 6.800.000 + 2.300.000 + 18.000.000 + 5.400.000 + 9.600.000 + 450.000 = 58.250.000 VNĐ. Bên thu là 30.000.000 + 45.600.000 + 27.800.000 = 103.400.000 VNĐ, cũng khớp dòng Cộng phát sinh.
**Nguồn tham khảo:** bang_ke_toan_multi_sheet.xlsx, sheet So_chi_ti_tai

### Case X-23
**Câu hỏi:** Số dư cuối kỳ theo sổ là bao nhiêu và tính bằng cách nào?
**Đáp án mong đợi:** 45.150.000 VNĐ, bằng Có trừ Nợ: 103.400.000 − 58.250.000.
**Nguồn tham khảo:** bang_ke_toan_multi_sheet.xlsx, sheet So_chi_ti_tai

### Case X-24
**Câu hỏi:** Vì sao số dư theo sổ lệch với ngân hàng, lệch bao nhiêu?
**Đáp án mong đợi:** Chênh lệch cần giải trình 27.800.000 VNĐ (sổ 45.150.000 so với ngân hàng 17.350.000), đúng bằng chứng từ CT-019 thu tiền khách hàng C chưa được ghi nhận.
**Nguồn tham khảo:** bang_ke_toan_multi_sheet.xlsx, sheet Doi_chieu_ngan_hang + So_chi_ti_tai

### Case X-25
**Câu hỏi:** Tháng 10 thu bao nhiêu tiền dịch vụ và từ những khách hàng nào?
**Đáp án mong đợi:** 103.400.000 VNĐ từ ba chứng từ thu: CT-013 khách hàng A 30.000.000, CT-016 khách hàng B 45.600.000, CT-019 khách hàng C 27.800.000.
**Nguồn tham khảo:** bang_ke_toan_multi_sheet.xlsx, sheet So_chi_ti_tai

## Nhóm F — định dạng đầu ra và tài liệu scan

### Case X-26
**Câu hỏi:** Nội dung trang 2 của file scan_vua_text_vua_anh.pdf nói về gì?
**Đáp án mong đợi:** Trang 2 là ảnh nên phải OCR mới đọc được; câu trả lời phải nêu được nội dung trang 2 và trích dẫn đúng trang 2, phân biệt được với trang 1 (có lớp text sẵn).
**Nguồn tham khảo:** scan_vua_text_vua_anh.pdf, trang 2

### Case X-27
**Câu hỏi:** Lập bảng so sánh biểu phí bản 1 và bản 2 gồm 3 cột: dịch vụ, mức bản 1, mức bản 2.
**Đáp án mong đợi:** Bảng đủ 4 dòng dịch vụ: cấp bản sao 5.000 và 8.000; công chứng hợp đồng 100.000 và 150.000; tra cứu hồ sơ 30.000 và 30.000; chứng thực chữ ký không có ở bản 1 và 12.000 ở bản 2. Không được thêm dòng tự bịa.
**Nguồn tham khảo:** quy_che_phi_v1.xlsx + quy_che_phi_v2.xlsx

### Case X-28
**Câu hỏi:** Trả lời gọn trong 2 dòng: phí tra cứu hồ sơ là bao nhiêu?
**Đáp án mong đợi:** 30.000 VNĐ cho mỗi lần tra cứu, không đổi giữa hai phiên bản.
**Nguồn tham khảo:** quy_che_phi_v2.xlsx, sheet Bieu_phi (PQ-03)

### Case X-29
**Câu hỏi:** Kỳ đối chiếu này là tháng nào và ai lập bảng?
**Đáp án mong đợi:** Kỳ đối chiếu tháng 10/2025, do Phòng Kế toán lập, cập nhật ngày 31/10/2025.
**Nguồn tham khảo:** bang_ke_toan_multi_sheet.xlsx, sheet Quy_uoc
