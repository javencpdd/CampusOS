# 用途：为当前 PowerShell、Git 和可选的 Windows 当前用户系统代理设置 HTTP/mixed 代理。
# 适用平台：Windows PowerShell 5.1+ 或 PowerShell 7+；Linux/WSL2 请使用 sh/proxy.sh。
# 基本用法：. .\sh\proxy.ps1 on；如需 Docker Desktop 读取系统代理，追加 -SystemProxy。
# 关闭方法：. .\sh\proxy.ps1 off；状态/测试：.\sh\proxy.ps1 status 或 test。
# 注意：开头的“. ”是 dot-source 语法；省略后环境变量不会保留在当前 PowerShell。
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('on', 'off', 'status', 'test', 'help', 'enable', 'disable', 'start', 'stop', 'check')]
    [string]$Command = 'help',

    [string]$ProxyHost = '127.0.0.1',

    [ValidateRange(1, 65535)]
    [int]$Port = 7897,

    [switch]$SystemProxy,

    [switch]$SkipGit
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:ProxyUri = "http://${ProxyHost}:${Port}"
$script:WinInetProxyServer = "http=${ProxyHost}:${Port};https=${ProxyHost}:${Port}"
$script:NoProxyValue = 'localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16'
$script:RepositoryRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$script:StatePath = Join-Path $script:RepositoryRoot '.campusos\run\proxy-windows-state.json'
$script:IsDotSourced = $MyInvocation.InvocationName -eq '.'
$script:ProxyEnvironmentNames = @(
    'http_proxy', 'https_proxy', 'all_proxy', 'ftp_proxy',
    'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'FTP_PROXY'
)
$script:NoProxyEnvironmentNames = @('no_proxy', 'NO_PROXY')

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "[OK]   $Message" -ForegroundColor Green
}

function Get-GitValueSnapshot {
    param([Parameter(Mandatory = $true)][string]$Key)

    if ($null -eq (Get-Command git -ErrorAction SilentlyContinue)) {
        return [pscustomobject]@{ Exists = $false; Value = $null }
    }

    $value = & git config --global --get $Key 2>$null
    $exists = $LASTEXITCODE -eq 0
    return [pscustomobject]@{
        Exists = $exists
        Value  = if ($exists) { ($value -join "`n") } else { $null }
    }
}

function Get-RegistryValueSnapshot {
    param(
        [Parameter(Mandatory = $true)]$Properties,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $property = $Properties.PSObject.Properties[$Name]
    return [pscustomobject]@{
        Exists = $null -ne $property
        Value  = if ($null -ne $property) { $property.Value } else { $null }
    }
}

function Get-SystemProxySnapshot {
    $path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
    $properties = Get-ItemProperty -LiteralPath $path
    return [pscustomobject]@{
        ProxyEnable = Get-RegistryValueSnapshot -Properties $properties -Name 'ProxyEnable'
        ProxyServer = Get-RegistryValueSnapshot -Properties $properties -Name 'ProxyServer'
    }
}

function New-ProxyState {
    return [pscustomobject][ordered]@{
        Version             = 1
        ManagedProxyUri     = $script:ProxyUri
        ManagedSystemServer = $script:WinInetProxyServer
        GitCaptured         = $true
        GitManaged          = $false
        GitHttp             = Get-GitValueSnapshot -Key 'http.proxy'
        GitHttps            = Get-GitValueSnapshot -Key 'https.proxy'
        SystemCaptured      = $false
        SystemManaged       = $false
        System              = $null
    }
}

function Read-ProxyState {
    if (-not (Test-Path -LiteralPath $script:StatePath -PathType Leaf)) {
        return $null
    }

    try {
        return Get-Content -LiteralPath $script:StatePath -Raw | ConvertFrom-Json
    }
    catch {
        throw "无法读取代理状态文件 '$script:StatePath'。请先备份后删除该文件，再重试。$($_.Exception.Message)"
    }
}

function Write-ProxyState {
    param([Parameter(Mandatory = $true)]$State)

    $stateDirectory = Split-Path -Parent $script:StatePath
    if (-not (Test-Path -LiteralPath $stateDirectory -PathType Container)) {
        New-Item -ItemType Directory -Path $stateDirectory -Force | Out-Null
    }
    $State | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $script:StatePath -Encoding UTF8
}

function Set-CurrentShellProxy {
    foreach ($name in $script:ProxyEnvironmentNames) {
        Set-Item -Path "Env:$name" -Value $script:ProxyUri
    }
    foreach ($name in $script:NoProxyEnvironmentNames) {
        Set-Item -Path "Env:$name" -Value $script:NoProxyValue
    }
}

function Clear-CurrentShellProxy {
    param([Parameter(Mandatory = $true)][string]$ManagedProxyUri)

    foreach ($name in $script:ProxyEnvironmentNames) {
        $item = Get-Item -Path "Env:$name" -ErrorAction SilentlyContinue
        if ($null -ne $item -and $item.Value -eq $ManagedProxyUri) {
            Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue
        }
    }
    foreach ($name in $script:NoProxyEnvironmentNames) {
        $item = Get-Item -Path "Env:$name" -ErrorAction SilentlyContinue
        if ($null -ne $item -and $item.Value -eq $script:NoProxyValue) {
            Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue
        }
    }
}

function Restore-GitValue {
    param(
        [Parameter(Mandatory = $true)][string]$Key,
        [Parameter(Mandatory = $true)]$Snapshot
    )

    if ($Snapshot.Exists) {
        & git config --global $Key ([string]$Snapshot.Value)
    }
    else {
        & git config --global --unset-all $Key 2>$null
        if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 5) {
            throw "清除 Git 配置 '$Key' 失败，退出码 $LASTEXITCODE。"
        }
    }
}

function Notify-WinInetProxyChange {
    if ($null -eq ('CampusOS.WinInetSettings' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

namespace CampusOS {
    public static class WinInetSettings {
        [DllImport("wininet.dll", SetLastError = true)]
        public static extern bool InternetSetOption(
            IntPtr hInternet,
            int dwOption,
            IntPtr lpBuffer,
            int dwBufferLength);
    }
}
'@
    }

    [void][CampusOS.WinInetSettings]::InternetSetOption([IntPtr]::Zero, 39, [IntPtr]::Zero, 0)
    [void][CampusOS.WinInetSettings]::InternetSetOption([IntPtr]::Zero, 37, [IntPtr]::Zero, 0)
}

function Enable-WindowsSystemProxy {
    $path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
    New-ItemProperty -LiteralPath $path -Name 'ProxyEnable' -PropertyType DWord -Value 1 -Force | Out-Null
    New-ItemProperty -LiteralPath $path -Name 'ProxyServer' -PropertyType String -Value $script:WinInetProxyServer -Force | Out-Null
    Notify-WinInetProxyChange
}

function Restore-RegistryValue {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][ValidateSet('DWord', 'String')][string]$PropertyType,
        [Parameter(Mandatory = $true)]$Snapshot
    )

    if ($Snapshot.Exists) {
        New-ItemProperty -LiteralPath $Path -Name $Name -PropertyType $PropertyType -Value $Snapshot.Value -Force | Out-Null
    }
    else {
        Remove-ItemProperty -LiteralPath $Path -Name $Name -ErrorAction SilentlyContinue
    }
}

function Restore-WindowsSystemProxy {
    param([Parameter(Mandatory = $true)]$Snapshot)

    $path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
    Restore-RegistryValue -Path $path -Name 'ProxyEnable' -PropertyType DWord -Snapshot $Snapshot.ProxyEnable
    Restore-RegistryValue -Path $path -Name 'ProxyServer' -PropertyType String -Snapshot $Snapshot.ProxyServer
    Notify-WinInetProxyChange
}

function Test-TcpEndpoint {
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $connection = $client.BeginConnect($ProxyHost, $Port, $null, $null)
        if (-not $connection.AsyncWaitHandle.WaitOne(3000, $false)) {
            return $false
        }
        $client.EndConnect($connection)
        return $true
    }
    catch {
        return $false
    }
    finally {
        $client.Close()
    }
}

function Test-ProxyConnection {
    Write-Info "检查代理监听：${ProxyHost}:${Port}"
    if (-not (Test-TcpEndpoint)) {
        Write-Warning "无法连接 ${ProxyHost}:${Port}。请先启动代理程序并确认端口类型是 HTTP/mixed。"
        return $false
    }
    Write-Success "本地代理端口可连接"

    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if ($null -eq $curl) {
        Write-Warning '未找到 curl.exe，只完成了本地端口检查。'
        return $true
    }

    $target = 'https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/hello-world:pull'
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        # Windows PowerShell 会把原生程序 stderr 包装成 ErrorRecord；连接失败应成为诊断结果，
        # 不能在全局 Stop 策略下提前终止整个代理脚本。
        $ErrorActionPreference = 'Continue'
        $result = & $curl.Source --silent --show-error --output NUL --write-out '%{http_code}' `
            --connect-timeout 10 --max-time 20 --proxy $script:ProxyUri $target 2>&1
        $curlExitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    $resultLines = @($result | ForEach-Object { $_.ToString().Trim() })
    $httpCode = [string]($resultLines | Where-Object { $_ -match '^\d{3}$' } | Select-Object -Last 1)
    if ([string]::IsNullOrWhiteSpace($httpCode)) {
        $httpCode = '<无>'
    }
    if ($curlExitCode -eq 0 -and $httpCode -match '^2\d\d$') {
        Write-Success "Docker Hub 认证端点可经代理访问（HTTP $httpCode）"
        return $true
    }

    Write-Warning "代理端口可连接，但 Docker Hub 测试失败（curl=$curlExitCode，HTTP=${httpCode}）。"
    return $false
}

function Show-ProxyStatus {
    $environmentProxy = (Get-Item Env:http_proxy -ErrorAction SilentlyContinue).Value
    if ([string]::IsNullOrWhiteSpace($environmentProxy)) {
        $environmentProxy = '<未设置>'
    }

    $gitHttp = Get-GitValueSnapshot -Key 'http.proxy'
    $gitValue = if ($gitHttp.Exists) { [string]$gitHttp.Value } else { '<未设置>' }

    $systemPath = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
    $system = Get-ItemProperty -LiteralPath $systemPath
    $enableProperty = $system.PSObject.Properties['ProxyEnable']
    $serverProperty = $system.PSObject.Properties['ProxyServer']
    $systemEnabled = if ($null -ne $enableProperty -and [int]$enableProperty.Value -eq 1) { '已开启' } else { '已关闭' }
    $systemServer = if ($null -ne $serverProperty) { [string]$serverProperty.Value } else { '<未设置>' }

    Write-Host ''
    Write-Host 'CampusOS Windows 代理状态' -ForegroundColor Cyan
    Write-Host "  当前 PowerShell http_proxy : $environmentProxy"
    Write-Host "  Git 全局 http.proxy        : $gitValue"
    Write-Host "  Windows 当前用户系统代理   : $systemEnabled"
    Write-Host "  Windows ProxyServer         : $systemServer"
    Write-Host "  本脚本恢复状态              : $(if (Test-Path -LiteralPath $script:StatePath) { $script:StatePath } else { '<无>' })"
    Write-Host ''
}

function Show-ProxyHelp {
    @'
CampusOS Windows 代理脚本

用途：为当前 PowerShell、Git，以及可选的 Windows 当前用户系统代理配置 HTTP 代理。

常用命令（仓库根目录）：
  . .\sh\proxy.ps1 on
      为当前 PowerShell 设置环境变量，并设置 Git 全局代理。

  . .\sh\proxy.ps1 on -SystemProxy
      在上述基础上设置 Windows 当前用户系统代理，供 Docker Desktop 的 System proxy 使用。

  .\sh\proxy.ps1 status
  .\sh\proxy.ps1 test

  . .\sh\proxy.ps1 off
      清除本脚本设置的当前 Shell 环境，并恢复开启前的 Git/Windows 系统代理。

可选参数：
  -ProxyHost 127.0.0.1  代理监听地址，默认 127.0.0.1
  -Port 7897            HTTP/mixed 代理端口，默认 7897
  -SkipGit              开启时不修改 Git 全局代理
  -SystemProxy          开启时同时修改 Windows 当前用户系统代理

注意：
  1. 开头的“. ”是 PowerShell dot-source 语法；省略后环境变量无法留在当前窗口。
  2. -SystemProxy 只修改 WinINET 当前用户代理，不直接修改 WinHTTP。
  3. Docker Desktop 已运行时，修改系统代理后还需执行 docker desktop restart。
  4. 脚本不修改 Windows OpenSSH 的 ~/.ssh/config；SSH 代理请按客户端单独配置。
'@ | Write-Host
}

function Enable-CampusOSProxy {
    if (-not $script:IsDotSourced) {
        Write-Warning '当前不是 dot-source 调用；Git/系统代理可以保存，但环境变量不会留在当前 PowerShell。正确用法：. .\sh\proxy.ps1 on'
    }

    $state = Read-ProxyState
    if ($null -eq $state) {
        $state = New-ProxyState
    }

    if ($SystemProxy -and -not [bool]$state.SystemCaptured) {
        $state.System = Get-SystemProxySnapshot
        $state.SystemCaptured = $true
    }

    $state.ManagedProxyUri = $script:ProxyUri
    $state.ManagedSystemServer = $script:WinInetProxyServer
    Write-ProxyState -State $state

    Set-CurrentShellProxy
    Write-Success "当前 PowerShell 代理已设置为 $script:ProxyUri"

    if (-not $SkipGit) {
        if ($null -eq (Get-Command git -ErrorAction SilentlyContinue)) {
            Write-Warning '未找到 git，跳过 Git 全局代理。'
        }
        else {
            & git config --global http.proxy $script:ProxyUri
            & git config --global https.proxy $script:ProxyUri
            $state.GitManaged = $true
            Write-ProxyState -State $state
            Write-Success 'Git 全局 HTTP/HTTPS 代理已设置'
        }
    }

    if ($SystemProxy) {
        Enable-WindowsSystemProxy
        $state.SystemManaged = $true
        Write-ProxyState -State $state
        Write-Success 'Windows 当前用户系统代理已设置'
        Write-Warning '若 Docker Desktop 已运行，请执行 docker desktop restart 让 Docker 引擎重新读取代理。'
    }

    [void](Test-ProxyConnection)
}

function Disable-CampusOSProxy {
    $state = Read-ProxyState
    $managedProxyUri = if ($null -ne $state) { [string]$state.ManagedProxyUri } else { $script:ProxyUri }
    Clear-CurrentShellProxy -ManagedProxyUri $managedProxyUri
    Write-Success '已清除本脚本设置的当前 PowerShell 代理环境变量'

    if ($null -ne $state -and [bool]$state.GitManaged) {
        if ($null -eq (Get-Command git -ErrorAction SilentlyContinue)) {
            throw '需要恢复 Git 代理，但当前找不到 git。请安装/修复 Git 后再次执行 off。'
        }
        Restore-GitValue -Key 'http.proxy' -Snapshot $state.GitHttp
        Restore-GitValue -Key 'https.proxy' -Snapshot $state.GitHttps
        $state.GitManaged = $false
        Write-ProxyState -State $state
        Write-Success '已恢复开启脚本前的 Git 全局代理配置'
    }
    elseif ($null -eq $state -and -not $SkipGit) {
        $gitHttp = Get-GitValueSnapshot -Key 'http.proxy'
        if ($gitHttp.Exists -and [string]$gitHttp.Value -eq $managedProxyUri) {
            & git config --global --unset-all http.proxy 2>$null
            & git config --global --unset-all https.proxy 2>$null
            Write-Success '已清除与当前参数匹配的 Git 全局代理'
        }
    }

    if ($null -ne $state -and [bool]$state.SystemManaged) {
        Restore-WindowsSystemProxy -Snapshot $state.System
        $state.SystemManaged = $false
        Write-ProxyState -State $state
        Write-Success '已恢复开启脚本前的 Windows 当前用户系统代理'
        Write-Warning '若 Docker Desktop 已运行，请执行 docker desktop restart 让 Docker 引擎重新读取代理。'
    }
    elseif ($null -eq $state -and $SystemProxy) {
        $path = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
        $properties = Get-ItemProperty -LiteralPath $path
        $server = $properties.PSObject.Properties['ProxyServer']
        if ($null -ne $server -and [string]$server.Value -eq $script:WinInetProxyServer) {
            New-ItemProperty -LiteralPath $path -Name 'ProxyEnable' -PropertyType DWord -Value 0 -Force | Out-Null
            Notify-WinInetProxyChange
            Write-Success '已关闭与当前参数匹配的 Windows 当前用户系统代理'
        }
    }

    if ($null -ne $state -and -not [bool]$state.GitManaged -and -not [bool]$state.SystemManaged) {
        Remove-Item -LiteralPath $script:StatePath -Force
    }
}

switch ($Command.ToLowerInvariant()) {
    { $_ -in @('on', 'enable', 'start') } { Enable-CampusOSProxy; break }
    { $_ -in @('off', 'disable', 'stop') } { Disable-CampusOSProxy; break }
    'status' { Show-ProxyStatus; break }
    { $_ -in @('test', 'check') } { [void](Test-ProxyConnection); break }
    default { Show-ProxyHelp; break }
}
