param(
    [ValidateSet("setup", "config", "build", "up", "rebuild", "infra-up", "lan-check", "ps", "logs", "test", "shell", "down", "stop-apps", "stop-native", "reset")]
    [string]$Command = "up",
    [string]$Service = "",
    [switch]$Start,
    [switch]$Confirm
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$TemplateFile = Join-Path $Root "deploy/docker/.env.dev.example"
$LocalEnvFile = Join-Path $Root "deploy/docker/.env.dev.local"
$EnvFile = if ($env:CAMPUSOS_DOCKER_DEV_ENV) {
    $env:CAMPUSOS_DOCKER_DEV_ENV
} elseif ($Command -eq "setup" -or (Test-Path $LocalEnvFile)) {
    $LocalEnvFile
} else {
    $TemplateFile
}
$ComposeFile = Join-Path $Root "compose.dev.yml"
$RunDir = if ($env:CAMPUSOS_RUN_DIR) { $env:CAMPUSOS_RUN_DIR } else { Join-Path $Root ".campusos/run" }
$NativePidFile = Join-Path $RunDir "native-dev.pid"
$NativeHandoff = $false
if (-not $env:CAMPUSOS_DEV_UID) {
    $env:CAMPUSOS_DEV_UID = "1000"
}
if (-not $env:CAMPUSOS_DEV_GID) {
    $env:CAMPUSOS_DEV_GID = "1000"
}
$ComposeArgs = @("compose", "--env-file", $EnvFile)
if ($env:CAMPUSOS_DOCKER_PROJECT) {
    $ComposeArgs += @("--project-name", $env:CAMPUSOS_DOCKER_PROJECT)
}
$ComposeArgs += @("-f", $ComposeFile)

function Get-FileSetting {
    param([string]$File, [string]$Key)
    if (-not (Test-Path $File)) {
        return ""
    }
    foreach ($Line in [System.IO.File]::ReadAllLines($File)) {
        if ($Line.StartsWith("$Key=")) {
            return $Line.Substring($Key.Length + 1).Trim().Trim('"')
        }
    }
    return ""
}

function Set-FileSetting {
    param([string]$File, [string]$Key, [string]$Value)
    $Lines = [System.Collections.Generic.List[string]]::new()
    $Found = $false
    foreach ($Line in [System.IO.File]::ReadAllLines($File)) {
        if ($Line.StartsWith("$Key=")) {
            $Lines.Add("$Key=$Value")
            $Found = $true
        } else {
            $Lines.Add($Line)
        }
    }
    if (-not $Found) {
        $Lines.Add("$Key=$Value")
    }
    $Utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllLines($File, $Lines, $Utf8NoBom)
}

function Import-ExistingLocalSettings {
    $SourceFile = if ($env:CAMPUSOS_DOCKER_DEV_IMPORT_SOURCE) {
        $env:CAMPUSOS_DOCKER_DEV_IMPORT_SOURCE
    } else {
        Join-Path $Root ".env"
    }
    if ($EnvFile -ne $LocalEnvFile -and -not $env:CAMPUSOS_DOCKER_DEV_IMPORT_SOURCE) {
        return
    }
    if (-not (Test-Path $SourceFile)) {
        return
    }

    $Keys = @(
        "POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD", "REDIS_PASSWORD",
        "CAMPUSOS_ENV", "CAMPUSOS_INSTANCE_MODE",
        "JWT_SECRET", "JWT_ACCESS_TTL", "JWT_REFRESH_TTL", "JWT_ISSUER",
        "AUTH_PASSWORD_HASH_ENABLED", "AUTH_BOOTSTRAP_ADMIN_SECRET",
        "AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN", "AUTH_CHALLENGE_ACTIVE_KEY_ID",
        "AUTH_CHALLENGE_HMAC_KEYS", "AUTH_CHALLENGE_IP_HASH_SECRET",
        "AUTH_SESSION_IP_HASH_SECRET", "AUTH_REFRESH_BODY_COMPAT",
        "AUTH_MFA_ACTIVE_KEY_ID", "AUTH_MFA_ENCRYPTION_KEYS", "AUTH_MFA_ISSUER",
        "EMAIL_PROVIDER", "EMAIL_SMTP_HOST", "EMAIL_SMTP_PORT", "EMAIL_SMTP_USERNAME",
        "EMAIL_SMTP_PASSWORD", "EMAIL_SMTP_FROM", "EMAIL_SMTP_TIMEOUT", "EMAIL_SMTP_STARTTLS"
    )
    $Imported = 0
    foreach ($Key in $Keys) {
        $Value = Get-FileSetting $SourceFile $Key
        if ($Value) {
            Set-FileSetting $EnvFile $Key $Value
            $Imported++
        }
    }
    if ($Imported -gt 0) {
        Write-Host "Imported $Imported shared runtime setting(s) from the existing root .env without printing values."
    }
}

function Initialize-Environment {
    if (Test-Path $EnvFile) {
        Write-Host "Docker development configuration already exists: $EnvFile"
        return $false
    }
    $Parent = Split-Path -Parent $EnvFile
    New-Item -ItemType Directory -Force -Path $Parent | Out-Null
    Copy-Item $TemplateFile $EnvFile
    Import-ExistingLocalSettings
    if ($env:OS -ne "Windows_NT") {
        & chmod 600 $EnvFile
        if ($LASTEXITCODE -ne 0) {
            exit $LASTEXITCODE
        }
    }
    Write-Host "Created local Docker development configuration: $EnvFile"
    Write-Host "Edit the ports and EMAIL_* settings, then run this setup command again."
    return $true
}

function Get-EnvironmentSetting {
    param([string]$Key, [string]$Fallback = "")
    $EnvironmentValue = [System.Environment]::GetEnvironmentVariable($Key)
    if ($EnvironmentValue) {
        return $EnvironmentValue
    }
    if (Test-Path $EnvFile) {
        foreach ($Line in [System.IO.File]::ReadAllLines($EnvFile)) {
            if ($Line.StartsWith("$Key=")) {
                $Value = $Line.Substring($Key.Length + 1).Trim().Trim('"')
                if ($Value) {
                    return $Value
                }
            }
        }
    }
    return $Fallback
}

function Assert-Port {
    param([string]$Key, [int]$Fallback)
    $Raw = Get-EnvironmentSetting $Key $Fallback.ToString()
    $Port = 0
    if (-not [int]::TryParse($Raw, [ref]$Port) -or $Port -lt 1 -or $Port -gt 65535) {
        throw "$Key must be an integer between 1 and 65535."
    }
    return $Port
}

function Assert-Environment {
    if (-not (Test-Path $EnvFile)) {
        throw "Docker development configuration does not exist: $EnvFile. Run '.\scripts\docker-dev.ps1 setup' first."
    }

    $Bind = Get-EnvironmentSetting "CAMPUSOS_DEV_BIND" "127.0.0.1"
    if ($Bind -ne "127.0.0.1") {
        throw "CAMPUSOS_DEV_BIND must remain 127.0.0.1 because this stack uses development credentials."
    }

    $AllowLan = (Get-EnvironmentSetting "CAMPUSOS_DEV_ALLOW_LAN" "false").ToLowerInvariant()
    if ($AllowLan -ne "true" -and $AllowLan -ne "false") {
        throw "CAMPUSOS_DEV_ALLOW_LAN must be true or false."
    }
    $SurfaceBinds = [ordered]@{
        CAMPUSOS_DEV_WEB_BIND = "127.0.0.1"
        CAMPUSOS_DEV_ADMIN_BIND = "127.0.0.1"
        CAMPUSOS_DEV_DOCS_BIND = "127.0.0.1"
    }
    $LanSurfaces = @()
    foreach ($Key in $SurfaceBinds.Keys) {
        $SurfaceBind = Get-EnvironmentSetting $Key $SurfaceBinds[$Key]
        if ($SurfaceBind -ne "127.0.0.1" -and $SurfaceBind -ne "0.0.0.0") {
            throw "$Key must be 127.0.0.1 or 0.0.0.0."
        }
        if ($SurfaceBind -eq "0.0.0.0") {
            if ($AllowLan -ne "true") {
                throw "$Key=0.0.0.0 requires CAMPUSOS_DEV_ALLOW_LAN=true."
            }
            $LanSurfaces += $Key
        }
    }
    if ($LanSurfaces.Count -gt 0) {
        Write-Warning "LAN exposure is enabled for $($LanSurfaces.Count) UI service(s); API and data services remain loopback-only."
    }

    $Ports = @{}
    $PortDefaults = [ordered]@{
        CAMPUSOS_DEV_WEB_PORT = 3000
        CAMPUSOS_DEV_ADMIN_PORT = 3001
        CAMPUSOS_DEV_DOCS_PORT = 3002
        CAMPUSOS_DEV_API_PORT = 8080
        CAMPUSOS_DEV_POSTGRES_PORT = 55432
        CAMPUSOS_DEV_REDIS_PORT = 56379
        CAMPUSOS_DEV_NATS_PORT = 54222
        CAMPUSOS_DEV_NATS_MONITOR_PORT = 58222
        CAMPUSOS_DEV_PGADMIN_PORT = 5050
    }
    foreach ($Key in $PortDefaults.Keys) {
        $Port = Assert-Port $Key $PortDefaults[$Key]
        if ($Ports.ContainsKey($Port)) {
            throw "$Key conflicts with $($Ports[$Port]) on port $Port."
        }
        $Ports[$Port] = $Key
    }

    $Provider = (Get-EnvironmentSetting "EMAIL_PROVIDER" "fake").ToLowerInvariant()
    switch ($Provider) {
        "fake" {
            Write-Host "Email provider: fake (registration requests succeed, but no verification email is sent)."
        }
        "smtp" {
            $HostName = Get-EnvironmentSetting "EMAIL_SMTP_HOST"
            $Port = Assert-Port "EMAIL_SMTP_PORT" 587
            $Username = Get-EnvironmentSetting "EMAIL_SMTP_USERNAME"
            $Password = Get-EnvironmentSetting "EMAIL_SMTP_PASSWORD"
            $From = Get-EnvironmentSetting "EMAIL_SMTP_FROM"
            $Timeout = Get-EnvironmentSetting "EMAIL_SMTP_TIMEOUT" "10s"
            $StartTLS = (Get-EnvironmentSetting "EMAIL_SMTP_STARTTLS" "true").ToLowerInvariant()
            if (-not $HostName -or -not $From) {
                throw "EMAIL_SMTP_HOST and EMAIL_SMTP_FROM are required when EMAIL_PROVIDER=smtp."
            }
            if (($Username -and -not $Password) -or (-not $Username -and $Password)) {
                throw "EMAIL_SMTP_USERNAME and EMAIL_SMTP_PASSWORD must either both be set or both be empty."
            }
            if (-not $Timeout) {
                throw "EMAIL_SMTP_TIMEOUT must not be empty."
            }
            if ($StartTLS -ne "true" -and $StartTLS -ne "false") {
                throw "EMAIL_SMTP_STARTTLS must be true or false."
            }
            Write-Host "Email provider: smtp (${HostName}:${Port}, from $From, STARTTLS=$StartTLS)."
        }
        default {
            throw "EMAIL_PROVIDER must be fake or smtp."
        }
    }
}

function Assert-Compose {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "Docker is not installed or is not available on PATH."
    }
    & docker compose version *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose v2 is required."
    }
}

function Assert-Docker {
    Assert-Compose
    & docker info *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker daemon is not running or the current user cannot access it."
    }
}

function Invoke-Compose {
    param([string[]]$Arguments)
    & docker @ComposeArgs @Arguments
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

function Start-DevelopmentStack {
    param([switch]$Build)

    $script:NativeHandoff = $false
    Stop-NativeDevelopment
    if ($script:NativeHandoff) {
        Wait-ApplicationPorts
    }
    $Arguments = @("up", "-d")
    if ($Build) {
        Write-Host "Rebuilding Docker development application images before startup."
        $Arguments += @("--build", "--force-recreate")
    } else {
        Write-Host "Starting with existing Docker development images (no registry-backed rebuild)."
        Write-Host "Use '.\scripts\docker-dev.ps1 rebuild' after dependency, Dockerfile, Compose build, or container entry-script changes."
        $Arguments += "--no-build"
    }
    $Arguments += @("--wait", "--wait-timeout", "600")
    if ($Build) {
        $Arguments += @("api", "web", "admin", "docs")
    }
    Invoke-Compose $Arguments
    Invoke-Compose @("ps")
}

function Start-Infrastructure {
    Invoke-Compose @("up", "-d", "--wait", "--wait-timeout", "600", "postgres", "redis", "nats")
    Invoke-Compose @("ps", "postgres", "redis", "nats")
}

function Stop-NativeDevelopment {
    if (-not (Test-Path $NativePidFile)) {
        return
    }

    $RawProcessId = (Get-Content -Raw $NativePidFile).Trim()
    $NativeProcessId = 0
    if (-not [int]::TryParse($RawProcessId, [ref]$NativeProcessId)) {
        Remove-Item -Force $NativePidFile
        return
    }

    $NativeProcess = Get-Process -Id $NativeProcessId -ErrorAction SilentlyContinue
    if (-not $NativeProcess) {
        Remove-Item -Force $NativePidFile
        return
    }

    $CommandLine = ""
    $ProcCommand = "/proc/$NativeProcessId/cmdline"
    if (Test-Path $ProcCommand) {
        $CommandLine = [System.Text.Encoding]::UTF8.GetString(
            [System.IO.File]::ReadAllBytes($ProcCommand)
        ).Replace([char]0, " ")
    } elseif (Get-Command Get-CimInstance -ErrorAction SilentlyContinue) {
        $CommandLine = (Get-CimInstance Win32_Process -Filter "ProcessId = $NativeProcessId").CommandLine
    }
    if (-not $CommandLine -or $CommandLine -notlike "*scripts/start-dev.sh*") {
        throw "Refusing to stop PID $NativeProcessId because it is not a tracked CampusOS native development process. Check $NativePidFile."
    }

    Write-Host "Stopping native CampusOS development services (PID $NativeProcessId)."
    $script:NativeHandoff = $true
    Stop-Process -Id $NativeProcessId
    for ($Attempt = 0; $Attempt -lt 100; $Attempt++) {
        if (-not (Get-Process -Id $NativeProcessId -ErrorAction SilentlyContinue)) {
            Remove-Item -Force $NativePidFile -ErrorAction SilentlyContinue
            return
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Native CampusOS development process $NativeProcessId did not stop within 10 seconds."
}

function Test-ApplicationPort {
    param([int]$Port)
    $Client = [System.Net.Sockets.TcpClient]::new()
    try {
        $Task = $Client.ConnectAsync("127.0.0.1", $Port)
        return $Task.Wait(100) -and $Client.Connected
    } catch {
        return $false
    } finally {
        $Client.Dispose()
    }
}

function Wait-ApplicationPorts {
    $Ports = @(
        (Assert-Port "CAMPUSOS_DEV_API_PORT" 8080),
        (Assert-Port "CAMPUSOS_DEV_WEB_PORT" 3000),
        (Assert-Port "CAMPUSOS_DEV_ADMIN_PORT" 3001),
        (Assert-Port "CAMPUSOS_DEV_DOCS_PORT" 3002)
    )
    $Busy = @()
    for ($Attempt = 0; $Attempt -lt 100; $Attempt++) {
        $Busy = @($Ports | Where-Object { Test-ApplicationPort $_ })
        if ($Busy.Count -eq 0) {
            return
        }
        Start-Sleep -Milliseconds 100
    }
    throw "CampusOS application port(s) remain occupied after native shutdown: $($Busy -join ' ')"
}

function Stop-ApplicationServices {
    Assert-Docker
    $Running = @(& docker @ComposeArgs ps --services --status running api web admin docs)
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    $Running = @($Running | Where-Object { $_ })
    if ($Running.Count -eq 0) {
        Write-Host "No Docker development application services are running."
        return
    }
    Write-Host "Stopping Docker development application services: $($Running -join ' ')"
    Invoke-Compose @("stop", "api", "web", "admin", "docs")
}

switch ($Command) {
    "setup" {
        if (Initialize-Environment) {
            Write-Host ""
            Write-Host "Next:"
            Write-Host "  1. Edit $EnvFile"
            Write-Host "  2. Keep EMAIL_PROVIDER=fake for API-only development, or configure smtp for real registration mail."
            Write-Host "  3. Run '.\scripts\docker-dev.ps1 setup -Start' to validate and start."
            exit 0
        }
        Assert-Environment
        Assert-Compose
        Invoke-Compose @("config", "--quiet")
        Write-Host "Docker development configuration is valid: $EnvFile"
        if ($Start) {
            Assert-Docker
            Start-DevelopmentStack -Build
        } else {
            Write-Host "Run '.\scripts\docker-dev.ps1 setup -Start' to start now, or '.\scripts\docker-dev.ps1 up' later."
        }
    }
    "config" {
        Assert-Environment
        Assert-Compose
        Invoke-Compose @("config", "--quiet")
        Write-Host "Docker development configuration is valid: $EnvFile"
    }
    "build" {
        Assert-Docker
        Invoke-Compose @("build", "api", "web", "admin", "docs")
    }
    "up" {
        Assert-Environment
        Assert-Docker
        Start-DevelopmentStack
    }
    "rebuild" {
        Assert-Environment
        Assert-Docker
        Start-DevelopmentStack -Build
    }
    "infra-up" {
        Assert-Environment
        Assert-Docker
        Start-Infrastructure
    }
    "lan-check" {
        $Python = $null
        foreach ($Candidate in @("python", "python3")) {
            $CandidateCommand = Get-Command $Candidate -ErrorAction SilentlyContinue
            if (-not $CandidateCommand) {
                continue
            }
            try {
                & $CandidateCommand.Source -c "import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)" 2>$null
                if ($LASTEXITCODE -eq 0) {
                    $Python = $CandidateCommand
                    break
                }
            }
            catch {
                continue
            }
        }
        if (-not $Python) {
            throw "Python 3.10 or newer is required for lan-check."
        }
        & $Python.Source (Join-Path $Root "scripts/check-lan-access.py") `
            --env-file $EnvFile `
            --compose-file $ComposeFile
        if ($LASTEXITCODE -ne 0) {
            exit $LASTEXITCODE
        }
    }
    "ps" {
        Invoke-Compose @("ps")
    }
    "logs" {
        $Arguments = @("logs", "-f", "--tail=200")
        if ($Service) {
            $Arguments += $Service
        }
        Invoke-Compose $Arguments
    }
    "test" {
        Invoke-Compose @(
            "run", "--rm", "--no-deps",
            "-e", "PLUGINS_DIR=data/plugins",
            "-e", "PLUGIN_DATA_DIR=.campusos/go-test/plugin_data",
            "-e", "MODULE_DATA_DIR=.campusos/go-test/module_data",
            "-e", "RESOURCE_DIR=data/resources",
            "api", "bash", "-c",
            "GOFLAGS=-buildvcs=false go test ./... -count=1"
        )
    }
    "shell" {
        Invoke-Compose @("exec", "api", "bash")
    }
    "down" {
        Stop-NativeDevelopment
        Invoke-Compose @("down")
    }
    "stop-apps" {
        Stop-ApplicationServices
    }
    "stop-native" {
        Stop-NativeDevelopment
    }
    "reset" {
        if (-not $Confirm) {
            throw "reset deletes only the campusos-dev containers and named volumes; pass -Confirm"
        }
        Stop-NativeDevelopment
        Invoke-Compose @("down", "--volumes", "--remove-orphans")
    }
}
