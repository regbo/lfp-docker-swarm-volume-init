$WORKING_DIR = Get-Location
$ErrorActionPreference = "Stop"

$PARENT_MODULE = $false
$TAG_APPEND = ""

$DIR = (Get-Item $PSScriptRoot)
$MODULE_DIR = (& { if ($PARENT_MODULE) { $DIR.Parent } else { $DIR } })
$MODULE_NAME = Split-Path $MODULE_DIR.FullName -Leaf
$PARENT_NAME = Split-Path $MODULE_DIR.Parent.FullName -Leaf
$REPOSITORY = "regbo/${PARENT_NAME}_${MODULE_NAME}"
if (![string]::IsNullOrEmpty($TAG_APPEND)) {
  $REPOSITORY = "${REPOSITORY}_${TAG_APPEND}"
}
$NOW = [math]::Round((Get-Date).ToFileTime() / 10000)
try {
  Write-Output "building:${REPOSITORY} in $DIR"
  Set-Location $DIR
  docker build . `
     -t "${REPOSITORY}:latest" `
     -t "${REPOSITORY}:${NOW}"
  docker push "${REPOSITORY}:latest"
  docker push "${REPOSITORY}:${NOW}"
} finally {
  Set-Location $WORKING_DIR
}
