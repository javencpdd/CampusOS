$ErrorActionPreference = "Stop"

if (Test-Path ".env") {
    Get-Content ".env" | ForEach-Object {
        $line = $_.Trim()
        if ($line -eq "" -or $line.StartsWith("#") -or -not $line.Contains("=")) {
            return
        }
        $parts = $line.Split("=", 2)
        [Environment]::SetEnvironmentVariable($parts[0].Trim(), $parts[1].Trim(), "Process")
    }
}

$PostgresPort = if ($env:POSTGRES_PORT) { [int]$env:POSTGRES_PORT } else { 5432 }
$RedisPort = if ($env:REDIS_PORT) { [int]$env:REDIS_PORT } else { 6379 }
$NatsClientPort = if ($env:NATS_CLIENT_PORT) { [int]$env:NATS_CLIENT_PORT } else { 4222 }

function Test-ServiceRunning([string]$Service) {
    $running = docker compose ps --services --filter status=running
    return (@($running) -contains $Service)
}

function Test-PortInUse([int]$Port) {
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $result = $client.BeginConnect("127.0.0.1", $Port, $null, $null)
        if (-not $result.AsyncWaitHandle.WaitOne(250, $false)) {
            return $false
        }
        $client.EndConnect($result)
        return $true
    }
    catch {
        return $false
    }
    finally {
        $client.Close()
    }
}

function Add-ServiceIfNeeded([string]$Service, [int]$Port, [string]$EnvName) {
    if (Test-ServiceRunning $Service) {
        Write-Host "==> $Service already running"
        return
    }

    if (Test-PortInUse $Port) {
        Write-Host "==> skip $Service`: localhost:$Port is already in use. Stop the local service or set $EnvName to another port."
        return
    }

    $script:Services += $Service
}

$Services = @()
Add-ServiceIfNeeded "postgres" $PostgresPort "POSTGRES_PORT"
Add-ServiceIfNeeded "redis" $RedisPort "REDIS_PORT"
Add-ServiceIfNeeded "nats" $NatsClientPort "NATS_CLIENT_PORT"

if ($Services.Count -eq 0) {
    Write-Host "==> no Docker infrastructure service needs to be started"
    exit 0
}

docker compose up -d $Services
