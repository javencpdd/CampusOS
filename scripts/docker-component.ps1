param(
    [ValidateSet("init", "config", "build", "up", "ps", "logs", "down")]
    [string]$Command,
    [ValidateSet("infra", "api", "web", "admin", "docs")]
    [string]$Component = "infra"
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$GlobalEnv = if ($env:CAMPUSOS_DOCKER_ENV) {
    $env:CAMPUSOS_DOCKER_ENV
} else {
    Join-Path $Root ".env.docker"
}
$ConfigDir = if ($env:CAMPUSOS_COMPONENT_CONFIG_DIR) {
    $env:CAMPUSOS_COMPONENT_CONFIG_DIR
} else {
    Join-Path $Root ".env.components"
}
$TemplateDir = Join-Path $Root "deploy/docker/components"

function Initialize-Configs {
    if (-not (Test-Path $GlobalEnv)) {
        $env:CAMPUSOS_DOCKER_ENV = $GlobalEnv
        & (Join-Path $Root "scripts/docker-deploy.ps1") init
        if ($LASTEXITCODE -ne 0) {
            exit $LASTEXITCODE
        }
    }
    New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
    foreach ($Name in @("infra", "api", "web", "admin", "docs")) {
        $Target = Join-Path $ConfigDir "$Name.env"
        if (-not (Test-Path $Target)) {
            Copy-Item (Join-Path $TemplateDir ".env.$Name.example") $Target
            Write-Host "Created component config: $Target"
        }
    }
}

function Ensure-Configs {
    if (-not (Test-Path $GlobalEnv) -or -not (Test-Path $ConfigDir)) {
        Initialize-Configs
    }
}

function Get-Setting {
    param([string]$Key, [string]$Fallback, [string]$ComponentEnv)
    $EnvironmentValue = [System.Environment]::GetEnvironmentVariable($Key)
    if ($EnvironmentValue) {
        return $EnvironmentValue
    }
    $Value = ""
    foreach ($File in @($GlobalEnv, $ComponentEnv)) {
        if (-not (Test-Path $File)) {
            continue
        }
        foreach ($Line in [System.IO.File]::ReadAllLines($File)) {
            if ($Line.StartsWith("$Key=")) {
                $Value = $Line.Substring($Key.Length + 1).Trim().Trim('"')
            }
        }
    }
    if ($Value) {
        return $Value
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

function Get-ComposeArgs {
    $ComponentEnv = Join-Path $ConfigDir "$Component.env"
    $Arguments = @(
        "compose",
        "--env-file", $GlobalEnv,
        "--env-file", $ComponentEnv
    )
    if ($env:CAMPUSOS_COMPONENT_PROJECT) {
        $Arguments += @("--project-name", $env:CAMPUSOS_COMPONENT_PROJECT)
    }
    $Arguments += @("-f", (Join-Path $TemplateDir "compose.$Component.yml"))
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

if ($Command -eq "init") {
    Initialize-Configs
    exit 0
}

Ensure-Configs
$ComponentEnv = Join-Path $ConfigDir "$Component.env"

switch ($Command) {
    "config" {
        Invoke-Compose @("config", "--quiet")
        Write-Host "Docker component configuration is valid: $Component"
    }
    "build" {
        if ($Component -eq "infra") {
            Write-Host "infra uses upstream images and has no CampusOS image build."
        } else {
            Invoke-Compose @("build", $Component)
        }
    }
    "up" {
        if ($Component -eq "infra" -or $Component -eq "api") {
            Ensure-Network (Get-Setting "CAMPUSOS_BACKEND_NETWORK" "campusos_backend" $ComponentEnv) -InternalNetwork
        }
        if ($Component -ne "infra") {
            Ensure-Network (Get-Setting "CAMPUSOS_EDGE_NETWORK" "campusos_edge" $ComponentEnv)
        }
        if ($Component -eq "infra") {
            Ensure-Volume (Get-Setting "CAMPUSOS_POSTGRES_VOLUME" "campusos_postgres-data" $ComponentEnv)
            Ensure-Volume (Get-Setting "CAMPUSOS_REDIS_VOLUME" "campusos_redis-data" $ComponentEnv)
            Ensure-Volume (Get-Setting "CAMPUSOS_NATS_VOLUME" "campusos_nats-data" $ComponentEnv)
        } elseif ($Component -eq "api") {
            Ensure-Volume (Get-Setting "CAMPUSOS_DATA_VOLUME" "campusos_campusos-data" $ComponentEnv)
        }
        Invoke-Compose @("up", "-d", "--build", "--wait", "--wait-timeout", "300")
        Invoke-Compose @("ps")
    }
    "ps" {
        Invoke-Compose @("ps")
    }
    "logs" {
        Invoke-Compose @("logs", "-f", "--tail=200")
    }
    "down" {
        Invoke-Compose @("down")
    }
}
