# Integration test script for health-aggregator
# This script runs the docker container and checks if it fails with the expected K8s error,
# which confirms it reached the Kubernetes config loading stage.

$image = "health-aggregator:latest"

Write-Host "Running integration test for $image..."

# Run container and capture output/error
$output = & docker run --rm $image 2>&1

$outputString = $output -join "`n"

Write-Host "Container output:"
Write-Host $outputString

# We expect it to fail because it's not in a K8s cluster, but it SHOULD reach the config loading part.
if ($outputString -like "*Error getting kubernetes config*") {
    Write-Host "Success: Container started and attempted to load K8s config."
    exit 0
} else {
    Write-Host "Failure: Container did not produce the expected K8s error message."
    exit 1
}
