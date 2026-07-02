#!/usr/bin/env bash

set -e

docker login ghcr.io

TAG=$(git rev-parse --short HEAD)
IMAGE=ghcr.io/konoplev/language-helper-bot

docker buildx build \
    --platform linux/amd64,linux/arm64 \
    --push \
    -t "$IMAGE:$TAG" \
    -t "$IMAGE:latest" \
    .

echo "$IMAGE:$TAG"
