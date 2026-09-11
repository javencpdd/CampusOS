# 用法：在 CampusOS 仓库根目录执行 .\migrations\tools\generate_er_windows.ps1
# 可选：-Check 只检查已生成的 PNG、SVG 和中文说明是否与 migrations 一致。
param(
    [string]$Migrations = "migrations",
    [string]$Out = "docs/architecture/database-er",
    [switch]$Check
)

$ErrorActionPreference = "Stop"
$Python = if (Get-Command py -ErrorAction SilentlyContinue) {
    @("py", "-3")
} elseif (Get-Command python -ErrorAction SilentlyContinue) {
    @("python")
} else {
    throw "未找到 Python 3。Windows 可执行：winget install Python.Python.3.12"
}

$Generator = Join-Path $PSScriptRoot "generate_er.py"
$Arguments = @($Generator, "--migrations", $Migrations, "--out", $Out)
if ($Check) {
    $Arguments += "--check"
}

Write-Host $(if ($Check) { "正在检查 CampusOS ER 产物..." } else { "正在生成 CampusOS ER 产物..." })
if ($Python.Count -eq 2) {
    & $Python[0] $Python[1] @Arguments
} else {
    & $Python[0] @Arguments
}
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

if (-not $Check) {
    Write-Host "已完成：$Out\CampusOS数据库ER图.png"
    Write-Host "已完成：$Out\CampusOS数据库ER图.svg"
    Write-Host "已完成：$Out\CampusOS数据库实体关系说明.md"
}
