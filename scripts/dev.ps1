$ErrorActionPreference = "Stop"

& "$PSScriptRoot/docker-up.ps1"
& "$PSScriptRoot/migrate.ps1" up

Write-Host "==> Starting CampusOS API"
go run ./cmd/server/main.go
