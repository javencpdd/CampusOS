param(
    [ValidateSet("init", "config", "build", "up", "ps", "logs", "backup", "restore", "down")]
    [string]$Command = "up",
    [string]$Service = "",
    [string]$Archive = "",
    [switch]$Confirm
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$EnvFile = if ($env:CAMPUSOS_DOCKER_ENV) {
    $env:CAMPUSOS_DOCKER_ENV
} else {
    Join-Path $Root ".env.docker"
}
$TemplateFile = Join-Path $Root "deploy/docker/.env.example"
$ComposeFile = Join-Path $Root "compose.deploy.yml"
$BackupDir = Join-Path $Root "backups"

function New-Hex {
    param([int]$Bytes)
    $Buffer = New-Object byte[] $Bytes
    $Generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $Generator.GetBytes($Buffer)
    } finally {
        $Generator.Dispose()
    }
    return (($Buffer | ForEach-Object { $_.ToString("x2") }) -join "")
}

function Initialize-Environment {
    if (Test-Path $EnvFile) {
        Write-Host "Docker environment already exists: $EnvFile"
        return
    }

    $Content = [System.IO.File]::ReadAllText($TemplateFile)
    $Content = $Content.Replace("__POSTGRES_PASSWORD__", (New-Hex 24))
    $Content = $Content.Replace("__JWT_SECRET__", (New-Hex 32))
    $Content = $Content.Replace("__BOOTSTRAP_ADMIN_SECRET__", (New-Hex 18))
    $Content = $Content.Replace("__CHALLENGE_HMAC_SECRET__", (New-Hex 32))
    $Content = $Content.Replace("__CHALLENGE_IP_HASH_SECRET__", (New-Hex 32))
    $Content = $Content.Replace("__SESSION_IP_HASH_SECRET__", (New-Hex 32))
    $Content = $Content.Replace("__MFA_ENCRYPTION_SECRET__", (New-Hex 32))
    $Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($EnvFile, $Content, $Utf8NoBom)
    if ($IsLinux -or $IsMacOS) {
        & chmod 600 $EnvFile
    }
    Write-Host "Created $EnvFile. Review email, public URL and production settings before network exposure."
}

function Ensure-Environment {
    if (-not (Test-Path $EnvFile)) {
        Initialize-Environment
    }
}

function Get-ComposeArgs {
    $Arguments = @("compose", "--env-file", $EnvFile)
    if ($env:CAMPUSOS_DOCKER_PROJECT) {
        $Arguments += @("--project-name", $env:CAMPUSOS_DOCKER_PROJECT)
    }
    $Arguments += @("-f", $ComposeFile)
    return $Arguments
}

function Invoke-Compose {
    param([string[]]$Arguments)
    $ComposeArgs = Get-ComposeArgs
    & docker @ComposeArgs @Arguments
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

function Get-EnvironmentSetting {
    param([string]$Key, [string]$Fallback)
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

function Ensure-Network {
    param([string]$Name, [switch]$InternalNetwork)
    & docker network inspect $Name *> $null
    if ($LASTEXITCODE -eq 0) {
        if ($InternalNetwork) {
            $IsInternal = (& docker network inspect --format "{{.Internal}}" $Name).Trim()
            if ($LASTEXITCODE -ne 0) {
                exit $LASTEXITCODE
            }
            if ($IsInternal -ne "true") {
                throw "Docker backend network '$Name' exists but is not internal. Stop attached CampusOS containers, remove only that network, then run this command again."
            }
        }
        return
    }
    $Arguments = @("network", "create")
    if ($InternalNetwork) {
        $Arguments += "--internal"
    }
    $Arguments += $Name
    & docker @Arguments | Out-Null
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    Write-Host "Created Docker network: $Name"
}

function Ensure-RuntimeNetworks {
    Ensure-Network (Get-EnvironmentSetting "CAMPUSOS_BACKEND_NETWORK" "campusos_backend") -InternalNetwork
    Ensure-Network (Get-EnvironmentSetting "CAMPUSOS_EDGE_NETWORK" "campusos_edge")
}

function Ensure-Volume {
    param([string]$Name)
    & docker volume inspect $Name *> $null
    if ($LASTEXITCODE -ne 0) {
        & docker volume create $Name | Out-Null
        if ($LASTEXITCODE -ne 0) {
            exit $LASTEXITCODE
        }
        Write-Host "Created Docker volume: $Name"
    }
}

function Ensure-RuntimeVolumes {
    Ensure-Volume (Get-EnvironmentSetting "CAMPUSOS_POSTGRES_VOLUME" "campusos_postgres-data")
    Ensure-Volume (Get-EnvironmentSetting "CAMPUSOS_REDIS_VOLUME" "campusos_redis-data")
    Ensure-Volume (Get-EnvironmentSetting "CAMPUSOS_NATS_VOLUME" "campusos_nats-data")
    Ensure-Volume (Get-EnvironmentSetting "CAMPUSOS_DATA_VOLUME" "campusos_campusos-data")
    Ensure-Volume (Get-EnvironmentSetting "CAMPUSOS_PGADMIN_VOLUME" "campusos_pgadmin-data")
}

function New-StackBackup {
    New-Item -ItemType Directory -Force -Path $BackupDir | Out-Null
    $ComposeArgs = Get-ComposeArgs
    $ContainerId = (& docker @ComposeArgs ps -q api).Trim()
    if (-not $ContainerId) {
        throw "CampusOS API container is not running."
    }

    $StagingDir = "/app/data/.backup-staging"
    & docker @ComposeArgs exec -T api sh -c "mkdir -p '$StagingDir' && rm -f '$StagingDir'/campusos-backup-*.tar.gz"
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    $BackupOutput = & docker @ComposeArgs exec -T api /app/scripts/backup.sh $StagingDir
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    $ArchivePath = ($BackupOutput | Select-Object -Last 1).Trim()
    if (-not $ArchivePath.StartsWith("$StagingDir/campusos-backup-")) {
        throw "CampusOS API returned an unexpected backup path: $ArchivePath"
    }
    & docker cp "${ContainerId}:${ArchivePath}" $BackupDir
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    Invoke-Compose @("exec", "-T", "api", "rm", "-f", $ArchivePath)
    $HostArchive = Join-Path $BackupDir ([System.IO.Path]::GetFileName($ArchivePath))
    if ($env:OS -ne "Windows_NT") {
        & chmod 0600 $HostArchive
        if ($LASTEXITCODE -ne 0) {
            exit $LASTEXITCODE
        }
    }
    Write-Host "Backup copied to $HostArchive"
}

function Restore-Stack {
    if (-not $Confirm -or -not $Archive) {
        throw "restore requires -Archive .\backups\<archive>.tar.gz -Confirm"
    }
    $ArchivePath = (Resolve-Path $Archive).Path
    $BackupRoot = (Resolve-Path $BackupDir).Path
    $BackupPrefix = $BackupRoot.TrimEnd([char[]]@("\", "/")) + [System.IO.Path]::DirectorySeparatorChar
    if (-not $ArchivePath.StartsWith($BackupPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Place the archive below $BackupRoot before restoring it."
    }
    $RelativePath = $ArchivePath.Substring($BackupPrefix.Length)
    $ContainerArchive = "/backups/" + $RelativePath.Replace("\", "/")

    Invoke-Compose @("build", "api")
    Ensure-RuntimeNetworks
    Ensure-RuntimeVolumes
    Invoke-Compose @("run", "--rm", "--no-deps", "maintenance", "/app/scripts/restore.sh", "verify", $ContainerArchive)

    $ComposeArgs = Get-ComposeArgs
    $ContainerId = (& docker @ComposeArgs ps -q api).Trim()
    if ($ContainerId) {
        Write-Host "Creating an automatic pre-restore safety backup."
        New-StackBackup
    } else {
        Write-Host "API is not running; no automatic pre-restore backup was created."
    }

    Invoke-Compose @("stop", "web", "admin", "api")
    Invoke-Compose @("up", "-d", "--wait", "--wait-timeout", "300", "postgres", "redis", "nats")
    Invoke-Compose @("run", "--rm", "--no-deps", "maintenance", "/app/scripts/docker-restore.sh", $ContainerArchive, "--confirm")
    Invoke-Compose @("up", "-d", "--wait", "--wait-timeout", "300")
    Invoke-Compose @("ps")
    Write-Host "Restore completed. Preserve the source .env.docker until login, MFA and integration checks pass."
}

switch ($Command) {
    "init" {
        Initialize-Environment
    }
    "config" {
        Ensure-Environment
        Invoke-Compose @("config", "--quiet")
        Write-Host "Docker deployment configuration is valid."
    }
    "build" {
        Ensure-Environment
        Invoke-Compose @("build", "api", "web", "admin", "docs")
    }
    "up" {
        Ensure-Environment
        Ensure-RuntimeNetworks
        Ensure-RuntimeVolumes
        Invoke-Compose @("up", "-d", "--build", "--wait", "--wait-timeout", "300")
        Invoke-Compose @("ps")
    }
    "ps" {
        Ensure-Environment
        Invoke-Compose @("ps")
    }
    "logs" {
        Ensure-Environment
        $Arguments = @("logs", "-f", "--tail=200")
        if ($Service) {
            $Arguments += $Service
        }
        Invoke-Compose $Arguments
    }
    "backup" {
        Ensure-Environment
        New-StackBackup
    }
    "restore" {
        Ensure-Environment
        Restore-Stack
    }
    "down" {
        Ensure-Environment
        Invoke-Compose @("down")
    }
}
