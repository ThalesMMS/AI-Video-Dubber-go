$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "go was not found in PATH"
    exit 1
}

New-Item -ItemType Directory -Force -Path "bin" | Out-Null

go mod download
go build -trimpath -o "bin/ai-video-dubber.exe" ./cmd/ai-video-dubber
go build -trimpath -o "bin/ai-video-dubber-cli.exe" ./cmd/ai-video-dubber-cli

Write-Host "Build complete:"
Write-Host "  $Root\bin\ai-video-dubber.exe"
Write-Host "  $Root\bin\ai-video-dubber-cli.exe"
