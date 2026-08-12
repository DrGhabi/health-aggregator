#!/bin/bash
# Integration test script for health-aggregator
# This script runs the docker container and checks if it fails with the expected K8s error,
# which confirms it reached the Kubernetes config loading stage.

image="health-aggregator:latest"

echo "Running integration test for $image..."

# Run container and capture output/error
# 2>&1 redirects stderr to stdout
output=$(docker run --rm "$image" 2>&1)

echo "Container output:"
echo "$output"

# We expect it to fail because it's not in a K8s cluster, but it SHOULD reach the config loading part.
if echo "$output" | grep -q "Error getting kubernetes config"; then
    echo "Success: Container started and attempted to load K8s config."
    exit 0
else
    echo "Failure: Container did not produce the expected K8s error message."
    exit 1
fi
