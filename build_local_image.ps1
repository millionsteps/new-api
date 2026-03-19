param(
  [string]$Image = "akon/new-api",
  [string]$Tag = "",
  [switch]$AlsoTagLatest,
  [switch]$Push,
  [switch]$PushLatest,
  [string]$HostProxy = "http://127.0.0.1:7897",
  [string]$HostAllProxy = "socks5://127.0.0.1:7897",
  [string]$HostNoProxy = "localhost,127.0.0.1,host.docker.internal",
  [int]$PushRetries = 3,
  [int]$PushRetryDelaySeconds = 10,
  [int]$DockerReadyRetries = 24,
  [int]$DockerReadyIntervalSeconds = 5
)

$ErrorActionPreference = "Stop"
$repoRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($repoRoot)) {
  $repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
}

Set-Location $repoRoot

$commit = (git -C $repoRoot rev-parse --short=8 HEAD).Trim()
if ([string]::IsNullOrWhiteSpace($Tag)) {
  $Tag = "{0}-{1}" -f (Get-Date -Format "yyyyMMdd"), $commit
}

$imageRef = "$Image`:$Tag"
$logDir = Join-Path $repoRoot "logs"
New-Item -ItemType Directory -Force $logDir | Out-Null

function Invoke-DockerPushWithRetry {
  param(
    [string]$ImageReference,
    [int]$Retries = 3,
    [int]$DelaySeconds = 10
  )

  $effectiveRetries = [Math]::Max(1, $Retries)
  $effectiveDelay = [Math]::Max(1, $DelaySeconds)
  $env:HTTP_PROXY = $HostProxy
  $env:HTTPS_PROXY = $HostProxy
  $env:ALL_PROXY = $HostAllProxy
  $env:NO_PROXY = $HostNoProxy
  $env:http_proxy = $HostProxy
  $env:https_proxy = $HostProxy
  $env:all_proxy = $HostAllProxy
  $env:no_proxy = $HostNoProxy

  for ($attempt = 1; $attempt -le $effectiveRetries; $attempt++) {
    Write-Host ""
    Write-Host ("==> Push image attempt {0}/{1}" -f $attempt, $effectiveRetries) -ForegroundColor Cyan
    Write-Host "INFO : push proxy HTTP/HTTPS=$HostProxy ALL_PROXY=$HostAllProxy NO_PROXY=$HostNoProxy" -ForegroundColor DarkYellow
    Write-Host "CMD  : docker push $ImageReference" -ForegroundColor DarkCyan
    docker push $ImageReference
    if ($LASTEXITCODE -eq 0) {
      return
    }

    Write-Host ("WARN : docker push failed with exit code {0}" -f $LASTEXITCODE) -ForegroundColor Yellow
    Write-Host "INFO : this usually means Docker Hub upload hit EOF or the proxy/network was unstable." -ForegroundColor DarkYellow

    if ($attempt -lt $effectiveRetries) {
      Write-Host ("INFO : waiting {0}s and retrying..." -f $effectiveDelay) -ForegroundColor DarkYellow
      Start-Sleep -Seconds $effectiveDelay
    }
  }

  throw "Failed to push $ImageReference after $effectiveRetries attempts."
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Yellow
Write-Host "new-api local image build" -ForegroundColor Yellow
Write-Host "========================================" -ForegroundColor Yellow
Write-Host "Repo   : $repoRoot"
Write-Host "Commit : $commit"
Write-Host "Image  : $imageRef"
Write-Host "Push   : $Push"
Write-Host "Latest : $AlsoTagLatest"
Write-Host "PushLatest: $PushLatest"
Write-Host "HostProxy: $HostProxy"
Write-Host "HostAllProxy: $HostAllProxy"
Write-Host "HostNoProxy: $HostNoProxy"
Write-Host "PushRetries: $PushRetries"
Write-Host "PushDelay : $PushRetryDelaySeconds"
Write-Host "Logs   : $logDir"
Write-Host ""

Write-Host "==> Checking Docker readiness..." -ForegroundColor Cyan
$dockerReady = $false
for ($attempt = 1; $attempt -le $DockerReadyRetries; $attempt++) {
  $versionOutput = docker version --format "Client={{.Client.Version}} Server={{.Server.Version}}" 2>&1
  $versionText = ($versionOutput | Out-String).Trim()
  if ($LASTEXITCODE -eq 0 -and $versionText -match "Server=\S+") {
    Write-Host $versionText
    $dockerReady = $true
    break
  }

  Write-Host ("[{0}/{1}] Docker not ready yet" -f $attempt, $DockerReadyRetries) -ForegroundColor Yellow
  if (-not [string]::IsNullOrWhiteSpace($versionText)) {
    Write-Host $versionText -ForegroundColor DarkYellow
  }

  if ($attempt -lt $DockerReadyRetries) {
    Write-Host ("Waiting {0}s and retrying..." -f $DockerReadyIntervalSeconds) -ForegroundColor Yellow
    Start-Sleep -Seconds $DockerReadyIntervalSeconds
  }
}

if (-not $dockerReady) {
  throw "Docker is not ready after retries. Please confirm Docker Desktop is fully started."
}

$buildArgs = @(
  "-ExecutionPolicy", "Bypass",
  "-File", (Join-Path $repoRoot "build_and_push_image.ps1"),
  "-Image", $Image,
  "-Tag", $Tag,
  "-HostProxy", $HostProxy,
  "-HostAllProxy", $HostAllProxy,
  "-HostNoProxy", $HostNoProxy
)

if ($Push) {
  $buildArgs += "-Push"
}
$buildArgs += @("-PushRetries", "$PushRetries", "-PushRetryDelaySeconds", "$PushRetryDelaySeconds")

powershell @buildArgs
if ($LASTEXITCODE -ne 0) {
  throw "Image build failed."
}

if ($AlsoTagLatest) {
  Write-Host ""
  Write-Host "==> Tag latest" -ForegroundColor Cyan
  Write-Host "CMD  : docker tag $imageRef $Image`:latest" -ForegroundColor DarkCyan
  docker tag $imageRef "$Image`:latest"
  if ($LASTEXITCODE -ne 0) {
    throw "Failed to tag latest."
  }
}

if ($PushLatest) {
  if (-not $AlsoTagLatest) {
    Write-Host ""
    Write-Host "==> Tag latest" -ForegroundColor Cyan
    Write-Host "CMD  : docker tag $imageRef $Image`:latest" -ForegroundColor DarkCyan
    docker tag $imageRef "$Image`:latest"
    if ($LASTEXITCODE -ne 0) {
      throw "Failed to tag latest."
    }
  }

  Write-Host ""
  Invoke-DockerPushWithRetry -ImageReference "$Image`:latest" -Retries $PushRetries -DelaySeconds $PushRetryDelaySeconds
}

Write-Host ""
Write-Host "Build finished: $imageRef" -ForegroundColor Green
