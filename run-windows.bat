@echo off

title openfsd

rem Single binary: both FSD and web (default when neither -fsd nor -web is passed).
set DATABASE_AUTO_MIGRATE=true
set DATABASE_SOURCE_NAME=openfsd.db?_pragma=busy_timeout(5000)^&_pragma=journal_mode(WAL)
set FSD_HTTP_SERVICE_ADDRESS=http://127.0.0.1:13618

go run ./cmd/openfsd
