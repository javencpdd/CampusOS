param(
    [ValidateSet("up", "down", "reset", "status")]
    [string]$Action = "up"
)

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

$MigrationsDir = if ($env:MIGRATIONS_DIR) { $env:MIGRATIONS_DIR } else { "migrations" }
$DbHost = if ($env:DB_HOST) { $env:DB_HOST } else { "localhost" }
$DbPort = if ($env:DB_PORT) { $env:DB_PORT } elseif ($env:POSTGRES_PORT) { $env:POSTGRES_PORT } else { "5432" }
$DbUser = if ($env:DB_USER) { $env:DB_USER } else { "campusos" }
$DbName = if ($env:DB_NAME) { $env:DB_NAME } else { "campusos" }
$DbPassword = if ($env:DB_PASSWORD) { $env:DB_PASSWORD } elseif ($env:POSTGRES_PASSWORD) { $env:POSTGRES_PASSWORD } else { "campusos_dev" }
$PostgresContainer = if ($env:POSTGRES_CONTAINER) { $env:POSTGRES_CONTAINER } else { "campusos-postgres" }
$PsqlMode = if ($env:PSQL_MODE) { $env:PSQL_MODE } else { "auto" }

function Test-DockerContainerAvailable {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        return $false
    }

    $names = & docker ps --format "{{.Names}}" 2>$null
    if ($LASTEXITCODE -ne 0) {
        return $false
    }

    return ($names -contains $PostgresContainer)
}

function Select-PsqlMode {
    switch ($PsqlMode) {
        "host" {
            if (-not (Get-Command psql -ErrorAction SilentlyContinue)) {
                throw "psql not found on host. Install postgresql-client or use PSQL_MODE=docker."
            }
            return "host"
        }
        "docker" {
            if (-not (Test-DockerContainerAvailable)) {
                throw "docker postgres container '$PostgresContainer' is not running."
            }
            return "docker"
        }
        "auto" {
            if (Get-Command psql -ErrorAction SilentlyContinue) {
                return "host"
            }
            if (Test-DockerContainerAvailable) {
                Write-Host "==> host psql not found; using docker exec $PostgresContainer psql"
                return "docker"
            }
            throw "psql not found on host and docker postgres container '$PostgresContainer' is not running."
        }
        default {
            throw "invalid PSQL_MODE '$PsqlMode' (expected auto, host, or docker)."
        }
    }
}

function Split-PsqlArgs {
    param([string[]]$PsqlArgs)

    $argsList = New-Object System.Collections.Generic.List[string]
    $file = $null

    for ($i = 0; $i -lt $PsqlArgs.Count; $i++) {
        $arg = $PsqlArgs[$i]
        if ($arg -eq "-f" -or $arg -eq "--file") {
            if ($i + 1 -ge $PsqlArgs.Count) {
                throw "missing SQL file after $arg"
            }
            $file = $PsqlArgs[$i + 1]
            $i++
            continue
        }

        if ($arg.StartsWith("--file=")) {
            $file = $arg.Substring("--file=".Length)
            continue
        }

        [void]$argsList.Add($arg)
    }

    return [PSCustomObject]@{
        Args = $argsList.ToArray()
        File = $file
    }
}

$script:SelectedPsqlMode = Select-PsqlMode

function Invoke-PsqlHost {
    param([string[]]$PsqlArgs)

    $oldPassword = $env:PGPASSWORD
    $env:PGPASSWORD = $DbPassword
    try {
        & psql -h $DbHost -p $DbPort -U $DbUser -d $DbName -v ON_ERROR_STOP=1 @PsqlArgs
        if ($LASTEXITCODE -ne 0) {
            throw "psql exited with code $LASTEXITCODE"
        }
    }
    finally {
        $env:PGPASSWORD = $oldPassword
    }
}

function Invoke-PsqlDocker {
    param([string[]]$PsqlArgs)

    $parsed = Split-PsqlArgs -PsqlArgs $PsqlArgs
    $dockerArgs = @(
        "exec",
        "-i",
        "-e",
        "PGPASSWORD=$DbPassword",
        $PostgresContainer,
        "psql",
        "-U",
        $DbUser,
        "-d",
        $DbName,
        "-v",
        "ON_ERROR_STOP=1"
    ) + $parsed.Args

    if ($parsed.File) {
        if (-not (Test-Path $parsed.File)) {
            throw "SQL file not found: $($parsed.File)"
        }
        Get-Content -Raw -Path $parsed.File | & docker @dockerArgs
    }
    else {
        & docker @dockerArgs
    }

    if ($LASTEXITCODE -ne 0) {
        throw "docker exec psql exited with code $LASTEXITCODE"
    }
}

function Invoke-Psql {
    param([string[]]$PsqlArgs)

    if ($script:SelectedPsqlMode -eq "host") {
        Invoke-PsqlHost -PsqlArgs $PsqlArgs
    }
    else {
        Invoke-PsqlDocker -PsqlArgs $PsqlArgs
    }
}

function Ensure-SchemaMigrations {
    Invoke-Psql @("-q", "-c", @"
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    VARCHAR(32) PRIMARY KEY,
  name       VARCHAR(255) NOT NULL,
  applied_at TIMESTAMP NOT NULL DEFAULT NOW()
);
"@)
}

function Get-Version([string]$Path) {
    $name = [System.IO.Path]::GetFileName($Path)
    return ($name -split "_")[0]
}

function Get-MigrationName([string]$Path) {
    $name = [System.IO.Path]::GetFileName($Path)
    return $name -replace "\.(up|down)\.sql$", ""
}

function Test-Applied([string]$Version) {
    $result = Invoke-Psql @("-tAc", "SELECT 1 FROM schema_migrations WHERE version = '$Version'")
    return ((($result -join "").Trim()) -eq "1")
}

function Mark-Applied([string]$Version, [string]$Name) {
    Invoke-Psql @("-q", "-c", @"
INSERT INTO schema_migrations (version, name, applied_at)
VALUES ('$Version', '$Name', NOW())
ON CONFLICT (version) DO UPDATE
  SET name = EXCLUDED.name,
      applied_at = EXCLUDED.applied_at;
"@)
}

function Unmark-Applied([string]$Version) {
    Invoke-Psql @("-q", "-c", "DELETE FROM schema_migrations WHERE version = '$Version';")
}

function Invoke-Up {
    Ensure-SchemaMigrations
    $files = Get-ChildItem -Path $MigrationsDir -Filter "*.up.sql" | Sort-Object Name
    foreach ($file in $files) {
        $version = Get-Version $file.FullName
        $name = Get-MigrationName $file.FullName
        if (Test-Applied $version) {
            Write-Host "==> skip $name"
            continue
        }
        Write-Host "==> apply $name"
        Invoke-Psql @("-f", $file.FullName)
        Mark-Applied $version $name
    }
}

function Invoke-Down {
    Ensure-SchemaMigrations
    $files = Get-ChildItem -Path $MigrationsDir -Filter "*.down.sql" | Sort-Object Name -Descending
    foreach ($file in $files) {
        $version = Get-Version $file.FullName
        $name = Get-MigrationName $file.FullName
        Write-Host "==> rollback $name"
        Invoke-Psql @("-f", $file.FullName)
        Unmark-Applied $version
    }
}

switch ($Action) {
    "up" { Invoke-Up }
    "down" { Invoke-Down }
    "reset" {
        Invoke-Down
        Invoke-Up
    }
    "status" {
        Ensure-SchemaMigrations
        Write-Host "==> schema_migrations"
        Invoke-Psql @("-c", "SELECT version, name, applied_at FROM schema_migrations ORDER BY version;")
    }
}
