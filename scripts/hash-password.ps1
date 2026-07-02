param(
    [string]$Password = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrEmpty($Password)) {
    $secure = Read-Host "Password" -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
        $Password = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
    }
}

go run ./scripts/hash-password.go $Password
