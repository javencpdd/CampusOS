#requires -Version 7.2
# Native Windows only. Run from a real console for targeted CTRL_BREAK_EVENT.
[CmdletBinding()]
param(
    [ValidateSet("target", "verify", "common", "documents", "seal", "exit")]
    [string]$Mode = "target",
    [string]$WorkDirectory
)

$ErrorActionPreference = "Stop"
if (-not [Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([Runtime.InteropServices.OSPlatform]::Windows)) {
    throw "Requires native Windows; WSL and PowerShell-on-Linux cannot certify this target."
}

$python = Get-Command python.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1
$runner = Join-Path $PSScriptRoot "v12-06-portable-drill.py"
$arguments = @($runner, $Mode)
if ($WorkDirectory) { $arguments += $WorkDirectory }
$arguments += "--native-windows"
& $python.Source @arguments
if ($LASTEXITCODE -ne 0) {
    throw "V12-06 $Mode failed (exit $LASTEXITCODE); retain the owned evidence directory."
}
