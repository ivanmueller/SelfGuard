# Build SelfGuard (service + GUI). Run from this folder:
#   powershell -ExecutionPolicy Bypass -File build.ps1
$ErrorActionPreference = "Stop"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed. Get it from https://go.dev/dl/ then re-run."
    exit 1
}

New-Item -ItemType Directory -Force -Path dist | Out-Null

Write-Host "Fetching dependencies..." -ForegroundColor Cyan
go mod tidy

$env:CGO_ENABLED = "0"

Write-Host "Building service (selfguard-svc.exe)..." -ForegroundColor Cyan
go build -ldflags "-s -w" -o dist\selfguard-svc.exe .

Write-Host "Building GUI (SelfGuard.exe)..." -ForegroundColor Cyan
go build -ldflags "-s -w -H windowsgui" -o dist\SelfGuard.exe .\gui

Copy-Item README.md dist\README.md -Force

Write-Host ""
Write-Host "Done. Both files are in .\dist :" -ForegroundColor Green
Write-Host "   SelfGuard.exe        <- double-click this (the control window)"
Write-Host "   selfguard-svc.exe    <- the background service (keep next to it)"
Write-Host ""
Write-Host "Open SelfGuard.exe and click 'Install & start protection'." -ForegroundColor Yellow
