param(
    [string]$Migrations = "migrations",
    [string]$Out = "docs/database/er",
    [ValidateSet("up", "all")]
    [string]$SqlMode = "up",
    [switch]$IncludeDown,
    [ValidateSet("svg", "pdf", "png")]
    [string]$Format = "svg"
)

$ErrorActionPreference = "Stop"

function Resolve-Python {
    if (Get-Command py -ErrorAction SilentlyContinue) { return @("py", "-3") }
    if (Get-Command python -ErrorAction SilentlyContinue) { return @("python") }
    throw "Python 3 was not found. Install it with: winget install Python.Python.3.12"
}

if (-not (Get-Command dot -ErrorAction SilentlyContinue)) {
    Write-Warning "Graphviz 'dot' is not on PATH. Install with: winget install Graphviz.Graphviz"
    Write-Warning "After installation, reopen PowerShell and verify: dot -V"
}

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$Generator = Join-Path $ScriptDir "generate_er.py"
if (-not (Test-Path $Generator)) {
    throw "generate_er.py not found next to this PowerShell script: $Generator"
}

$pythonCmd = Resolve-Python
$argsList = @($Generator, "--migrations", $Migrations, "--out", $Out, "--sql-mode", $SqlMode, "--format", $Format)
if ($IncludeDown) { $argsList += "--include-down" }

Write-Host "Generating ER diagrams..."
Write-Host "  migrations: $Migrations"
Write-Host "  sql mode:   $SqlMode"
Write-Host "  output:     $Out"

if ($pythonCmd.Count -eq 2) {
    & $pythonCmd[0] $pythonCmd[1] @argsList
} else {
    & $pythonCmd[0] @argsList
}

if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "Done. Open: $Out\\campusos_er_full.svg"
