param(
    [string]$Ref = "main"
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    throw "GitHub CLI is required. Install it and run 'gh auth login'."
}

gh workflow run ci-deploy.yml --ref $Ref
if ($LASTEXITCODE -ne 0) {
    throw "Failed to trigger the deployment workflow."
}

Write-Host "Deployment requested for '$Ref'."
Write-Host "Monitor it with: gh run watch"
