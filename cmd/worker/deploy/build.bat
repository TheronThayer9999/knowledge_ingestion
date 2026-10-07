@echo off
rem Build image worker tu GOC REPO (build context) - chay file nay tu bat ky dau.
setlocal
cd /d "%~dp0..\..\.."
docker build -f cmd/worker/deploy/Dockerfile -t knowledge-worker:latest .
if errorlevel 1 (
    echo BUILD FAILED
    pause
    exit /b 1
)
echo BUILD OK: knowledge-worker:latest
