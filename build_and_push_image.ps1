param(
  [string]$Image = "akon/new-api",
  [string]$Tag = "20260319-32ece0ee",
  [string]$ContainerProxy = "http://host.docker.internal:7897",
  [string]$ContainerAllProxy = "socks5://host.docker.internal:7897",
  [string]$HostProxy = "http://127.0.0.1:7897",
  [string]$HostAllProxy = "socks5://127.0.0.1:7897",
  [string]$HostNoProxy = "localhost,127.0.0.1,host.docker.internal",
  [string]$GoProxy = "https://goproxy.cn,direct",
  [int]$PushRetries = 3,
  [int]$PushRetryDelaySeconds = 10,
  [switch]$Push
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$repoRoot = $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($repoRoot)) {
  $repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
}
$versionFile = Join-Path $repoRoot "VERSION"
$version = ""
if (Test-Path $versionFile) {
  $versionContent = Get-Content $versionFile -Raw
  if ($null -ne $versionContent) {
    $version = $versionContent.Trim()
  }
}
if ([string]::IsNullOrWhiteSpace($version)) {
  $version = (git -C $repoRoot rev-parse --short=8 HEAD).Trim()
}
$imageRef = "$Image`:$Tag"
$logDir = Join-Path $repoRoot "logs"
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$logFile = Join-Path $logDir "build-image-$timestamp.log"

function Assert-LastExitCode {
  param([string]$StepName)
  if ($LASTEXITCODE -ne 0) {
    throw "$StepName failed with exit code $LASTEXITCODE"
  }
}

function Write-Step {
  param([string]$Message)
  $now = Get-Date -Format "HH:mm:ss"
  Write-Host ""
  Write-Host "[$now] ==> $Message" -ForegroundColor Cyan
}

function Write-Command {
  param([string]$Message)
  Write-Host "CMD  : $Message" -ForegroundColor DarkCyan
}

function Test-CommandExists {
  param([string]$CommandName)
  return [bool](Get-Command $CommandName -ErrorAction SilentlyContinue)
}

function Resolve-ToolPath {
  param(
    [string]$CommandName,
    [string[]]$CandidatePaths = @()
  )

  $command = Get-Command $CommandName -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
    return $command.Source
  }

  foreach ($candidate in $CandidatePaths) {
    if ([string]::IsNullOrWhiteSpace($candidate)) {
      continue
    }
    $expandedCandidate = [Environment]::ExpandEnvironmentVariables($candidate)
    if (Test-Path $expandedCandidate) {
      return $expandedCandidate
    }
  }

  return $null
}

function Set-HostProxyEnvironment {
  param(
    [string]$HttpProxy,
    [string]$AllProxy,
    [string]$NoProxy
  )

  $env:HTTP_PROXY = $HttpProxy
  $env:HTTPS_PROXY = $HttpProxy
  $env:ALL_PROXY = $AllProxy
  $env:NO_PROXY = $NoProxy
  $env:http_proxy = $HttpProxy
  $env:https_proxy = $HttpProxy
  $env:all_proxy = $AllProxy
  $env:no_proxy = $NoProxy
}

function Invoke-DockerPushWithRetry {
  param(
    [string]$ImageReference,
    [int]$Retries = 3,
    [int]$DelaySeconds = 10
  )

  $effectiveRetries = [Math]::Max(1, $Retries)
  $effectiveDelay = [Math]::Max(1, $DelaySeconds)
  Set-HostProxyEnvironment -HttpProxy $HostProxy -AllProxy $HostAllProxy -NoProxy $HostNoProxy

  for ($attempt = 1; $attempt -le $effectiveRetries; $attempt++) {
    Write-Step "Push image attempt $attempt/$effectiveRetries"
    Write-Host "INFO : push proxy HTTP/HTTPS=$HostProxy ALL_PROXY=$HostAllProxy NO_PROXY=$HostNoProxy" -ForegroundColor DarkYellow
    Write-Command "docker push $ImageReference"
    docker push $ImageReference
    if ($LASTEXITCODE -eq 0) {
      return
    }

    Write-Host "WARN : docker push failed with exit code $LASTEXITCODE" -ForegroundColor Yellow
    Write-Host "INFO : this is often caused by Docker Hub network/proxy instability or EOF during layer upload." -ForegroundColor DarkYellow

    if ($attempt -lt $effectiveRetries) {
      Write-Host "INFO : waiting $effectiveDelay seconds before retry..." -ForegroundColor DarkYellow
      Start-Sleep -Seconds $effectiveDelay
    }
  }

  throw "docker push failed after $effectiveRetries attempts. Please check Docker Hub connectivity, proxy, or try again later."
}

New-Item -ItemType Directory -Force $logDir | Out-Null
Start-Transcript -Path $logFile -Force | Out-Null

Write-Step "Build start"
Write-Host "Repo : $repoRoot"
Write-Host "Image: $imageRef"
Write-Host "Log  : $logFile"

Set-Location $repoRoot
New-Item -ItemType Directory -Force (Join-Path $repoRoot "out") | Out-Null

Write-Step "Build web/dist on host"
Set-Location (Join-Path $repoRoot "web")
Remove-Item -Recurse -Force "dist" -ErrorAction SilentlyContinue

$env:HTTP_PROXY = $HostProxy
$env:HTTPS_PROXY = $HostProxy
$env:ALL_PROXY = $HostAllProxy
$env:NO_PROXY = $HostNoProxy
$env:http_proxy = $HostProxy
$env:https_proxy = $HostProxy
$env:all_proxy = $HostAllProxy
$env:no_proxy = $HostNoProxy
$env:VITE_REACT_APP_VERSION = $version
$env:DISABLE_ESLINT_PLUGIN = "true"

$frontendBuilt = $false
$bunPath = Resolve-ToolPath -CommandName "bun" -CandidatePaths @(
  "$env:USERPROFILE\.bun\bin\bun.exe",
  "C:\Users\admin\.bun\bin\bun.exe"
)
$pnpmPath = Resolve-ToolPath -CommandName "pnpm" -CandidatePaths @(
  "$env:APPDATA\npm\pnpm.cmd",
  "$env:ProgramFiles\nodejs\pnpm.cmd"
)
$npmPath = Resolve-ToolPath -CommandName "npm" -CandidatePaths @(
  "$env:APPDATA\npm\npm.cmd",
  "$env:ProgramFiles\nodejs\npm.cmd"
)

if (-not [string]::IsNullOrWhiteSpace($bunPath)) {
  Write-Host "INFO : local bun detected at $bunPath" -ForegroundColor DarkYellow
  Write-Command "`"$bunPath`" install --frozen-lockfile --verbose"
  & $bunPath install --frozen-lockfile --verbose
  Assert-LastExitCode "bun install"
  Write-Command "`"$bunPath`" run build"
  & $bunPath run build
  Assert-LastExitCode "bun run build"
  $frontendBuilt = $true
} elseif (-not [string]::IsNullOrWhiteSpace($pnpmPath)) {
  Write-Host "INFO : bun not found, using local pnpm at $pnpmPath" -ForegroundColor DarkYellow
  Write-Command "`"$pnpmPath`" install --no-frozen-lockfile"
  & $pnpmPath install --no-frozen-lockfile
  Assert-LastExitCode "pnpm install"
  Write-Command "`"$pnpmPath`" run build"
  & $pnpmPath run build
  Assert-LastExitCode "pnpm run build"
  $frontendBuilt = $true
} elseif (-not [string]::IsNullOrWhiteSpace($npmPath)) {
  Write-Host "INFO : bun/pnpm not found, using local npm at $npmPath" -ForegroundColor DarkYellow
  Write-Command "`"$npmPath`" install --legacy-peer-deps --no-audit --no-fund"
  & $npmPath install --legacy-peer-deps --no-audit --no-fund
  Assert-LastExitCode "npm install"
  Write-Command "`"$npmPath`" run build"
  & $npmPath run build
  Assert-LastExitCode "npm run build"
  $frontendBuilt = $true
}

if (-not $frontendBuilt) {
  throw "No local frontend build tool found. Please install bun, pnpm, or npm."
}

if (-not (Test-Path (Join-Path $repoRoot "web\dist"))) {
  throw "Frontend build finished but web/dist was not generated."
}

Write-Step "Build linux/amd64 binary on host"
Set-HostProxyEnvironment -HttpProxy $HostProxy -AllProxy $HostAllProxy -NoProxy $HostNoProxy
$env:GOPROXY = $GoProxy
$env:GOSUMDB = "sum.golang.org"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$env:GOEXPERIMENT = "greenteagc"
Set-Location $repoRoot

Write-Command "go mod download -x"
go mod download -x
Assert-LastExitCode "go mod download"
Write-Command "go build -v -ldflags ""-s -w -X github.com/QuantumNous/new-api/common.Version=$version"" -o out/new-api"
go build -v -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o out/new-api
Assert-LastExitCode "go build"

Write-Step "Build runtime image"
$env:DOCKER_BUILDKIT = "0"
Write-Command "docker build -f Dockerfile.runtime -t $imageRef ."
docker build `
  -f Dockerfile.runtime `
  -t $imageRef `
  --build-arg HTTP_PROXY=$ContainerProxy `
  --build-arg HTTPS_PROXY=$ContainerProxy `
  --build-arg ALL_PROXY=$ContainerAllProxy `
  --build-arg NO_PROXY=localhost,127.0.0.1,host.docker.internal `
  .
Assert-LastExitCode "docker build"

if ($Push) {
  Invoke-DockerPushWithRetry -ImageReference $imageRef -Retries $PushRetries -DelaySeconds $PushRetryDelaySeconds
}

Write-Step "Build finished"
Write-Host "Done : $imageRef" -ForegroundColor Green
Write-Host "Log  : $logFile" -ForegroundColor Green
Stop-Transcript | Out-Null
