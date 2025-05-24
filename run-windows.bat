@echo off

title openfsd

start /b cmd /c "set DATABASE_AUTO_MIGRATE=true&& set DATABASE_SOURCE_NAME=openfsd.db?_pragma=busy_timeout(5000)^&_pragma=journal_mode(WAL)&& go run ."

powershell -Command "$ProgressPreference = 'SilentlyContinue'; while (-not (Test-NetConnection -ComputerName localhost -Port 13618 -InformationLevel Quiet)) { Start-Sleep -Seconds 1 }" >nul 2>&1

cmd /c "cd web&& set FSD_HTTP_SERVICE_ADDRESS=http://localhost:13618&& set DATABASE_SOURCE_NAME=../openfsd.db?_pragma=busy_timeout(5000)^&_pragma=journal_mode(WAL)&& go run ."
