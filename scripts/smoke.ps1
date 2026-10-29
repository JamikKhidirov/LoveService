# LoveService end-to-end smoke test (PowerShell)
# Usage: .\scripts\smoke.ps1 [-BaseUrl http://localhost:8080]
param(
    [string]$BaseUrl = "http://localhost:8080"
)

$ErrorActionPreference = "Stop"
$BaseUrl = $BaseUrl.TrimEnd("/")

function Invoke-Api {
    param(
        [string]$Method,
        [string]$Path,
        [string]$Token = "",
        $Body = $null
    )
    $headers = @{}
    if ($Token) { $headers["Authorization"] = "Bearer $Token" }
    $params = @{
        Method      = $Method
        Uri         = "$BaseUrl$Path"
        Headers     = $headers
        TimeoutSec  = 30
    }
    if ($null -ne $Body) {
        $params["ContentType"] = "application/json"
        $params["Body"] = ($Body | ConvertTo-Json -Depth 8)
    }
    return Invoke-RestMethod @params
}

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw "ASSERT FAILED: $Message" }
}

Write-Host "== health / docs =="
foreach ($path in @("/health", "/ready", "/docs", "/openapi.yaml")) {
    $response = Invoke-WebRequest -Uri "$BaseUrl$path" -UseBasicParsing -TimeoutSec 20
    Assert-True ($response.StatusCode -eq 200) "$path returned $($response.StatusCode)"
    Write-Host "  OK $path"
}

Write-Host "== demo login =="
$login = Invoke-Api -Method POST -Path "/api/v1/auth/login" -Body @{
    email = "demo@loveservice.app"; password = "LoveDemo123!"
}
$token = $login.data.tokens.access_token
$userId = $login.data.user.id
Assert-True ([bool]$token) "no access token"
Write-Host "  OK user $userId"

Write-Host "== profile + location =="
Invoke-Api -Method POST -Path "/api/v1/profiles/me/location" -Token $token -Body @{ lat = 55.7558; lng = 37.6173 } | Out-Null
$me = Invoke-Api -Method GET -Path "/api/v1/auth/me" -Token $token
Assert-True ([bool]$me.data.profile) "profile missing"
Write-Host "  OK $($me.data.profile.display_name)"

Write-Host "== discovery =="
$discovery = Invoke-Api -Method GET -Path "/api/v1/discovery?lat=55.7558&lng=37.6173&radius_km=5&expand=true&limit=10" -Token $token
$count = @($discovery.data.profiles).Count
Write-Host "  OK candidates=$count radius=$($discovery.data.expanded_to_radius_km)"

Write-Host "== chat / notifications / export =="
foreach ($path in @("/api/v1/conversations", "/api/v1/matches", "/api/v1/notifications", "/api/v1/swipes/limits", "/api/v1/account/export")) {
    $result = Invoke-Api -Method GET -Path $path -Token $token
    Assert-True ($null -ne $result.data) "$path has no data"
    Write-Host "  OK $path"
}

Write-Host "== technical admin =="
$adminLogin = Invoke-Api -Method POST -Path "/api/v1/auth/login" -Body @{
    email = "admin@loveservice.app"; password = "AdminDemo123!"
}
$adminToken = $adminLogin.data.tokens.access_token
$stats = Invoke-Api -Method GET -Path "/api/v1/admin/stats" -Token $adminToken
Assert-True ($null -ne $stats.data.users) "admin stats missing"
Write-Host "  OK users=$($stats.data.users) legal_requests=$($stats.data.legal_requests)"

$audit = Invoke-Api -Method GET -Path "/api/v1/admin/audit-logs?limit=5" -Token $adminToken
Assert-True ($null -ne $audit.data.items) "audit log missing"
Write-Host "  OK audit entries=$(@($audit.data.items).Count)"

Write-Host "== integration suite =="
go test -tags=integration ./tests/integration/... -count=1
if ($LASTEXITCODE -ne 0) { throw "integration tests failed" }

Write-Host "`nALL SMOKE CHECKS PASSED"
