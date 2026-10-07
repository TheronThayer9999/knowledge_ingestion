# ==========================================
# STAGE 1: BUILDER (Chỉ dùng để compile code)
# ==========================================
# Dùng image có đầy đủ Golang compiler
FROM golang:1.23-alpine AS builder

# Tạo thư mục làm việc
WORKDIR /app

# Copy file go.mod và go.sum vào trước để tận dụng Docker Cache
# (Chỉ cài lại dependencies khi file go.mod thay đổi)
COPY go.mod go.sum ./

# Tải dependencies
RUN go mod download

# Copy toàn bộ source code vào
COPY . .

# Build ứng dụng
# CGO_ENABLED=0: Biên dịch tĩnh (static binary) để chạy được trên Alpine/Scratch
# GOOS=linux: Đảm bảo build ra binary chạy trên môi trường Linux
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .


# ==========================================
# STAGE 2: RUNNER (Môi trường chạy thực tế)
# ==========================================
# Dùng image Alpine siêu nhẹ (chỉ khoảng 5MB)
FROM alpine:latest

# Cài đặt CA certificates (Rất quan trọng nếu app của bạn có gọi API HTTPS ra bên ngoài)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Chỉ copy DUY NHẤT file binary 'main' từ Stage 1 sang Stage 2
COPY --from=builder /app/main .

# Khai báo port (Giả sử app Go của bạn chạy port 8080)
EXPOSE 8080

# Chạy ứng dụng khi container start
CMD ["./main"]