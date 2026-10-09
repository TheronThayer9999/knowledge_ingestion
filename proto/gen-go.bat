@echo off
rem Gen Go stubs tu file proto cho gRPC server.
rem
rem Yeu cau truoc khi chay:
rem   1. protoc trong PATH (tai: https://github.com/protocolbuffers/protobuf/releases)
rem   2. protoc-gen-go va protoc-gen-go-grpc trong %%USERPROFILE%%\go\bin:
rem        go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
rem        go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
rem
rem Chay tu thu muc goc repo:  proto\gen-go.bat
rem Sau khi gen xong: go build ./... de kiem tra.

setlocal
cd /d %~dp0..
set PATH=%PATH%;%USERPROFILE%\go\bin

where protoc >nul 2>nul
if errorlevel 1 (
  echo [loi] khong tim thay protoc trong PATH. Tai o link tren roi giai nen, them thu muc bin vao PATH.
  exit /b 1
)

protoc -Iproto ^
  --go_out=src/controller/grpchandler/internal/gen --go_opt=paths=source_relative ^
  --go-grpc_out=src/controller/grpchandler/internal/gen --go-grpc_opt=paths=source_relative ^
  proto/knowledge/v1/knowledge.proto
if errorlevel 1 (
  echo [loi] gen that bai.
  exit /b 1
)

echo [ok] da gen xong vao src/controller/grpchandler/internal/gen
echo.
echo Python (chay tren may agent, thay . bang / neu Linux):
echo   python -m grpc_tools.protoc -Iproto --python_out=. --grpc_python_out=. proto/knowledge/v1/knowledge.proto
