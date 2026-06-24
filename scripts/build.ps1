$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root
New-Item -ItemType Directory -Force -Path "bin" | Out-Null

go mod download
go build -trimpath -o "bin/ai-video-dubber.exe" ./cmd/ai-video-dubber
go build -trimpath -o "bin/ai-video-dubber-cli.exe" ./cmd/ai-video-dubber-cli

Write-Host "Build concluído:"
Write-Host "  $Root\bin\ai-video-dubber.exe"
Write-Host "  $Root\bin\ai-video-dubber-cli.exe"
