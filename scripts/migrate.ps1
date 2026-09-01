# 用法：.\scripts\migrate.ps1 up|down|reset|status|check
# reset 会清空目标数据库 public schema，仅允许 development/test，且必须设置
# CAMPUSOS_RESET_CONFIRM 为当前 DB_NAME。up/down 会校验迁移文件配对与 SHA-256。
param(
    [ValidateSet("up", "down", "reset", "status", "check")]
    [string]$Action = "up"
)

$ErrorActionPreference = "Stop"

if ($env:CAMPUSOS_SKIP_DOTENV -ne "true" -and (Test-Path ".env")) {
    Get-Content ".env" | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#") -and $line.Contains("=")) {
            $parts = $line.Split("=", 2)
            [Environment]::SetEnvironmentVariable($parts[0].Trim(), $parts[1].Trim(), "Process")
        }
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
$script:LockOwner = "$($env:COMPUTERNAME):$PID"
$script:LockHeld = $false

function Test-DockerContainerAvailable {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        return $false
    }
    $names = & docker ps --format "{{.Names}}" 2>$null
    return ($LASTEXITCODE -eq 0 -and $names -contains $PostgresContainer)
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
            throw "psql is unavailable and PostgreSQL container '$PostgresContainer' is not running."
        }
        default { throw "invalid PSQL_MODE '$PsqlMode' (expected auto, host, or docker)." }
    }
}

function Split-PsqlArgs {
    param([string[]]$PsqlArgs)
    $argsList = [Collections.Generic.List[string]]::new()
    $file = $null
    for ($i = 0; $i -lt $PsqlArgs.Count; $i++) {
        $arg = $PsqlArgs[$i]
        if ($arg -in @("-f", "--file")) {
            if ($i + 1 -ge $PsqlArgs.Count) { throw "missing SQL file after $arg" }
            $file = $PsqlArgs[++$i]
        }
        elseif ($arg.StartsWith("--file=")) {
            $file = $arg.Substring("--file=".Length)
        }
        else {
            $argsList.Add($arg)
        }
    }
    [PSCustomObject]@{ Args = $argsList.ToArray(); File = $file }
}

$script:SelectedPsqlMode = Select-PsqlMode

function Invoke-Psql {
    param([string[]]$PsqlArgs)

    if ($script:SelectedPsqlMode -eq "host") {
        $oldPassword = $env:PGPASSWORD
        $env:PGPASSWORD = $DbPassword
        try {
            & psql -h $DbHost -p $DbPort -U $DbUser -d $DbName -v ON_ERROR_STOP=1 @PsqlArgs
            if ($LASTEXITCODE -ne 0) { throw "psql exited with code $LASTEXITCODE" }
        }
        finally {
            $env:PGPASSWORD = $oldPassword
        }
        return
    }

    $parsed = Split-PsqlArgs -PsqlArgs $PsqlArgs
    $dockerArgs = @(
        "exec", "-i", "-e", "PGPASSWORD=$DbPassword", $PostgresContainer,
        "psql", "-U", $DbUser, "-d", $DbName, "-v", "ON_ERROR_STOP=1"
    ) + $parsed.Args

    if ($parsed.File) {
        if (-not (Test-Path -LiteralPath $parsed.File)) { throw "SQL file not found: $($parsed.File)" }
        Get-Content -Raw -LiteralPath $parsed.File | & docker @dockerArgs
    }
    else {
        & docker @dockerArgs
    }
    if ($LASTEXITCODE -ne 0) { throw "docker exec psql exited with code $LASTEXITCODE" }
}

function Get-SqlScalar([string]$Sql) {
    $value = Invoke-Psql @("-qAtc", $Sql)
    return (($value | Out-String).Trim())
}

function ConvertTo-SqlLiteral([string]$Value) {
    return $Value.Replace("'", "''")
}

function Get-Version([string]$Path) {
    return ([IO.Path]::GetFileName($Path) -split "_")[0]
}

function Get-MigrationName([string]$Path) {
    return [regex]::Replace([IO.Path]::GetFileName($Path), "\.(up|down)\.sql$", "")
}

function Assert-MigrationFiles {
    $upFiles = @(Get-ChildItem -LiteralPath $MigrationsDir -Filter "*.up.sql" | Sort-Object Name)
    $downFiles = @(Get-ChildItem -LiteralPath $MigrationsDir -Filter "*.down.sql" | Sort-Object Name)
    if ($upFiles.Count -eq 0) { throw "No up migrations found in $MigrationsDir" }
    if ($upFiles.Count -ne $downFiles.Count) { throw "migration up/down file count mismatch" }

    $seen = [Collections.Generic.HashSet[string]]::new()
    foreach ($file in $upFiles) {
        $version = Get-Version $file.FullName
        if ($version -notmatch "^[0-9]{6}$") { throw "invalid migration version: $($file.Name)" }
        if (-not $seen.Add($version)) { throw "duplicate migration version: $version" }
        $downPath = $file.FullName -replace "\.up\.sql$", ".down.sql"
        if (-not (Test-Path -LiteralPath $downPath)) { throw "missing down migration for $($file.Name)" }
    }
}

function Ensure-MigrationMetadata {
    Invoke-Psql @("-q", "-c", @"
CREATE TABLE IF NOT EXISTS public.schema_migrations (
  version      VARCHAR(32) PRIMARY KEY,
  name         VARCHAR(255) NOT NULL,
  checksum     CHAR(64) NOT NULL,
  execution_ms BIGINT NOT NULL DEFAULT 0 CHECK (execution_ms >= 0),
  executor     VARCHAR(128) NOT NULL DEFAULT 'unknown',
  applied_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS public.schema_migration_locks (
  id          SMALLINT PRIMARY KEY CHECK (id = 1),
  owner_name  VARCHAR(160) NOT NULL,
  acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
"@) | Out-Null

    $hasChecksum = Get-SqlScalar @"
SELECT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema='public' AND table_name='schema_migrations' AND column_name='checksum'
);
"@
    if ($hasChecksum -ne "t") {
        throw "legacy schema_migrations detected in '$DbName'. Use CAMPUSOS_ENV=development and CAMPUSOS_RESET_CONFIRM=$DbName, then run migrate.ps1 reset."
    }
}

function Acquire-MigrationLock {
    if ($env:CAMPUSOS_MIGRATION_LOCK_FORCE -eq "true") {
        Invoke-Psql @("-q", "-c", "DELETE FROM public.schema_migration_locks WHERE id=1;") | Out-Null
    }
    $owner = ConvertTo-SqlLiteral $script:LockOwner
    $acquired = Get-SqlScalar "INSERT INTO public.schema_migration_locks(id,owner_name) VALUES (1,'$owner') ON CONFLICT (id) DO NOTHING RETURNING owner_name;"
    if ($acquired -ne $script:LockOwner) {
        $current = Get-SqlScalar "SELECT owner_name || ' since ' || acquired_at FROM public.schema_migration_locks WHERE id=1;"
        throw "migration lock is held by $current"
    }
    $script:LockHeld = $true
}

function Release-MigrationLock {
    if ($script:LockHeld) {
        $owner = ConvertTo-SqlLiteral $script:LockOwner
        try { Invoke-Psql @("-q", "-c", "DELETE FROM public.schema_migration_locks WHERE id=1 AND owner_name='$owner';") | Out-Null } catch {}
        $script:LockHeld = $false
    }
}

function Invoke-AtomicMigration([string]$Path, [string]$MetadataSql) {
    $temporary = [IO.Path]::GetTempFileName()
    try {
        $newline = [Environment]::NewLine
        $sql = "BEGIN;" + $newline + [IO.File]::ReadAllText($Path) + $newline + $MetadataSql + $newline + "COMMIT;" + $newline
        [IO.File]::WriteAllText($temporary, $sql, [Text.UTF8Encoding]::new($false))
        Invoke-Psql @("-q", "-f", $temporary) | Out-Null
    }
    finally {
        Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
    }
}

function Test-AppliedChecksums {
    foreach ($file in Get-ChildItem -LiteralPath $MigrationsDir -Filter "*.up.sql" | Sort-Object Name) {
        $version = Get-Version $file.FullName
        $stored = Get-SqlScalar "SELECT checksum FROM public.schema_migrations WHERE version='$version';"
        if (-not $stored) { continue }
        $expected = (Get-FileHash -Algorithm SHA256 -LiteralPath $file.FullName).Hash.ToLowerInvariant()
        if ($stored -ne $expected) {
            throw "migration drift: $($file.Name) checksum=$expected recorded=$stored"
        }
    }
}

function Invoke-Up {
    Assert-MigrationFiles
    Ensure-MigrationMetadata
    Acquire-MigrationLock
    try {
        $owner = ConvertTo-SqlLiteral $script:LockOwner
        foreach ($file in Get-ChildItem -LiteralPath $MigrationsDir -Filter "*.up.sql" | Sort-Object Name) {
            $version = Get-Version $file.FullName
            $name = Get-MigrationName $file.FullName
            $checksum = (Get-FileHash -Algorithm SHA256 -LiteralPath $file.FullName).Hash.ToLowerInvariant()
            $storedChecksum = Get-SqlScalar "SELECT checksum FROM public.schema_migrations WHERE version='$version';"
            if ($storedChecksum) {
                $storedName = Get-SqlScalar "SELECT name FROM public.schema_migrations WHERE version='$version';"
                if ($storedChecksum -ne $checksum -or $storedName -ne $name) {
                    throw "migration drift: $version recorded as $storedName/$storedChecksum, file is $name/$checksum"
                }
                Write-Host "==> skip $name"
                continue
            }

            Write-Host "==> apply $name"
            $started = [Diagnostics.Stopwatch]::StartNew()
            $escapedName = ConvertTo-SqlLiteral $name
            Invoke-AtomicMigration $file.FullName @"
INSERT INTO public.schema_migrations(version,name,checksum,execution_ms,executor,applied_at)
VALUES ('$version','$escapedName','$checksum',0,'$owner',NOW());
"@
            $started.Stop()
            Invoke-Psql @("-q", "-c", "UPDATE public.schema_migrations SET execution_ms=$($started.ElapsedMilliseconds) WHERE version='$version';") | Out-Null
        }
    }
    finally {
        Release-MigrationLock
    }
}

function Invoke-Down {
    Assert-MigrationFiles
    Ensure-MigrationMetadata
    Acquire-MigrationLock
    try {
        $version = Get-SqlScalar "SELECT version FROM public.schema_migrations ORDER BY version DESC LIMIT 1;"
        if (-not $version) {
            Write-Host "No applied migration to roll back."
            return
        }
        $matches = @(Get-ChildItem -LiteralPath $MigrationsDir -Filter ($version + "_*.down.sql"))
        if ($matches.Count -ne 1) { throw "missing or ambiguous down migration for applied version $version" }
        $name = Get-MigrationName $matches[0].FullName
        Write-Host "==> rollback $name"
        Invoke-AtomicMigration $matches[0].FullName "DELETE FROM public.schema_migrations WHERE version='$version';"
    }
    finally {
        Release-MigrationLock
    }
}

function Reset-Database {
    if ($env:CAMPUSOS_ENV -notin @("development", "test")) {
        throw "reset is restricted to CAMPUSOS_ENV=development or test"
    }
    if ($env:CAMPUSOS_RESET_CONFIRM -ne $DbName) {
        throw "reset requires CAMPUSOS_RESET_CONFIRM=$DbName"
    }
    Write-Host "==> reset public schema in database $DbName"
    Invoke-Psql @("-q", "-c", "DROP SCHEMA public CASCADE; CREATE SCHEMA public;") | Out-Null
    Invoke-Up
}

switch ($Action) {
    "up" { Invoke-Up }
    "down" { Invoke-Down }
    "reset" { Reset-Database }
    "status" {
        Ensure-MigrationMetadata
        Test-AppliedChecksums
        Write-Host "==> schema_migrations"
        Invoke-Psql @("-c", "SELECT version,name,left(checksum,12) AS checksum,execution_ms,executor,applied_at FROM public.schema_migrations ORDER BY version;")
    }
    "check" {
        Assert-MigrationFiles
        Ensure-MigrationMetadata
        Test-AppliedChecksums
        Write-Host "migration files and recorded checksums are valid."
    }
}
