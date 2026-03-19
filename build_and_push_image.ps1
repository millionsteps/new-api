param(
  [string]$Image = "akon/new-api",
  [string]$Tag = "20260319-32ece0ee",
  [string]$ContainerProxy = "http://host.docker.internal:7897",
  [string]$ContainerAllProxy = "socks5://host.docker.internal:7897",
  [string]$HostProxy = "http://127.0.0.1:7897",
  [string]$HostAllProxy = "socks5://127.0.0.1:7897",
  [string]$GoProxy = "https://goproxy.cn,direct",
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

function Assert-LastExitCode {
  param([string]$StepName)
  if ($LASTEXITCODE -ne 0) {
    throw "$StepName failed with exit code $LASTEXITCODE"
  }
}

Write-Host "==> Repo: $repoRoot"
Write-Host "==> Image: $imageRef"

Set-Location $repoRoot
New-Item -ItemType Directory -Force (Join-Path $repoRoot "out") | Out-Null

Write-Host "==> Build web/dist with Bun container"
$volumeName = "new-api-web-node-modules-" + [Guid]::NewGuid().ToString("N").Substring(0, 8)
docker volume create $volumeName | Out-Null
Assert-LastExitCode "Create Bun volume"

try {
  $frontendBuilt = $false
  foreach ($attempt in 1..2) {
    Write-Host "==> Frontend attempt $attempt"
    docker run --rm `
      -v "${repoRoot}:/repo" `
      -v "${volumeName}:/repo/web/node_modules" `
      -w /repo/web `
      -e HTTP_PROXY=$ContainerProxy `
      -e HTTPS_PROXY=$ContainerProxy `
      -e ALL_PROXY=$ContainerAllProxy `
      oven/bun:latest `
      sh -lc 'rm -rf dist && bun install --frozen-lockfile && DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION=$(cat /repo/VERSION) bun run build'
    if ($LASTEXITCODE -eq 0 -and (Test-Path (Join-Path $repoRoot "web\dist"))) {
      $frontendBuilt = $true
      break
    }
  }

  if (-not $frontendBuilt) {
    throw "Frontend build failed after retries"
  }
}
finally {
  docker volume rm -f $volumeName | Out-Null
}

Write-Host "==> Build linux/amd64 binary on host"
$env:HTTP_PROXY = $HostProxy
$env:HTTPS_PROXY = $HostProxy
$env:ALL_PROXY = $HostAllProxy
$env:GOPROXY = $GoProxy
$env:GOSUMDB = "sum.golang.org"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$env:GOEXPERIMENT = "greenteagc"

go mod download
Assert-LastExitCode "go mod download"
go build -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=$version" -o out/new-api
Assert-LastExitCode "go build"

Write-Host "==> Build runtime image"
$env:DOCKER_BUILDKIT = "0"
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
  Write-Host "==> Push image"
  docker push $imageRef
  Assert-LastExitCode "docker push"
}

Write-Host "==> Done: $imageRef"
