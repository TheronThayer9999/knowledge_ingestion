@echo off
rem Chay worker container tu image knowledge-worker:latest.
rem Yeu cau: postgres + seaweedfs dang chay va resolve duoc tu trong container.
setlocal
set IMAGE=knowledge-worker:latest
set NAME=knowledge-worker
rem Ten docker network chua postgres + seaweedfs (vd seaweed-network).
rem De trong neu container tu resolve duoc host trong configs/config.json.
set NETWORK=
set ROOT=%~dp0..\..\..

rem Xoa container cu cung ten de chay lai idempotent (loi thi bo qua).
docker stop %NAME% >nul 2>&1
docker rm %NAME% >nul 2>&1

if defined NETWORK (
    set NETFLAG=--network %NETWORK%
) else (
    set NETFLAG=
)

docker run -d --name %NAME% --restart unless-stopped %NETFLAG% ^
  --add-host=host.docker.internal:host-gateway ^
  -v "%ROOT%\cmd\worker\deploy\config.container.json:/app/configs/config.json:ro" ^
  %IMAGE%
if errorlevel 1 (
    echo START FAILED
    pause
    exit /b 1
)
echo STARTED: %NAME%
docker logs --tail 20 %NAME%
