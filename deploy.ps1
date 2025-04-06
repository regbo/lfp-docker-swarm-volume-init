# Stop on error
$ErrorActionPreference = "Stop"

# Timestamp in milliseconds
$timestamp = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$imageName = "regbo/lfp-docker-swarm-volume-init"
$tagLatest = "${imageName}:latest"
$tagTimestamp = "${imageName}:$timestamp"

Write-Host "Building image with Jib..."
./gradlew jibDockerBuild

Write-Host "Tagging image..."
docker tag $imageName $tagTimestamp
docker tag $imageName $tagLatest

Write-Host "Pushing to Docker Hub..."
docker push $tagTimestamp
docker push $tagLatest

Write-Host "Done. Tags pushed:"
Write-Host " - $tagLatest"
Write-Host " - $tagTimestamp"
