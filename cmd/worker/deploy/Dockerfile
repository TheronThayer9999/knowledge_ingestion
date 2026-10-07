# ========== STAGE 1: BUILDER ==========
# Lưu ý: mọi đường dẫn COPY bên dưới tính từ GỐC REPO (build context)
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy module files trước để tận dụng cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code (cmd/, src/, configs/, schema/... đều nằm ở gốc repo)
COPY . .

# Build binary worker (build theo package, KHÔNG build theo file .go)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/worker ./cmd/worker

# ========== STAGE 2: RUNNER ==========
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/worker ./worker

# Nếu worker đọc file config khi chạy thì mở dòng này:
# COPY configs/ ./configs/

ENTRYPOINT ["./worker"]