param(
    [ValidateSet("config", "build", "up", "ps", "logs", "test", "shell", "down", "reset")]
    [string]$Command = "up",
    [string]$Service = "",
    [switch]$Confirm
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$EnvFile = if ($env:CAMPUSOS_DOCKER_DEV_ENV) {
    $env:CAMPUSOS_DOCKER_DEV_ENV
} else {
    Join-Path $Root "deploy/docker/.env.dev.example"
}
$ComposeFile = Join-Path $Root "compose.dev.yml"
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

function Invoke-Compose {
    param([string[]]$Arguments)
    & docker @ComposeArgs @Arguments
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

switch ($Command) {
    "config" {
        Invoke-Compose @("config", "--quiet")
        Write-Host "Docker development configuration is valid."
    }
    "build" {
        Invoke-Compose @("build", "api", "web", "admin", "docs")
    }
    "up" {
        Invoke-Compose @("up", "-d", "--build", "--wait", "--wait-timeout", "600")
        Invoke-Compose @("ps")
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
            "-e", "PLUGINS_DIR=/workspace/data/plugins",
            "-e", "PLUGIN_DATA_DIR=/tmp/campusos-go-test/plugin_data",
            "-e", "MODULE_DATA_DIR=/tmp/campusos-go-test/module_data",
            "-e", "RESOURCE_DIR=/workspace/data/resources",
            "api", "bash", "-c",
            "GOCACHE=/go-cache/build GOMODCACHE=/go-cache/modules GOFLAGS=-buildvcs=false go test ./... -count=1"
        )
    }
    "shell" {
        Invoke-Compose @("exec", "api", "bash")
    }
    "down" {
        Invoke-Compose @("down")
    }
    "reset" {
        if (-not $Confirm) {
            throw "reset deletes only the campusos-dev containers and named volumes; pass -Confirm"
        }
        Invoke-Compose @("down", "--volumes", "--remove-orphans")
    }
}
