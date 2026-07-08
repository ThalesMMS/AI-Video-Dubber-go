$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "go was not found in PATH"
    exit 1
}

go run ./cmd/ai-video-dubber @args
