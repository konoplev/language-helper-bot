#!/usr/bin/env bash

# Architecture configuration
# Available options:
#   linux/amd64  - for x86_64 servers (Intel/AMD)
#   linux/arm64  - for ARM64 servers (64-bit Raspberry Pi, Apple Silicon, AWS Graviton)
PLATFORM="linux/arm64"

# Extract traefik arch from platform (amd64 or arm64)
TRAEFIK_ARCH="${PLATFORM#linux/}"

docker login ghcr.io

# ng test --watch=false
# if [ $? -ne 0 ]; then exit 1; fi
TAG=`git rev-parse --short HEAD`
docker build --platform $PLATFORM \
    --build-arg BUILD_ARCH=$PLATFORM \
    --build-arg TRAEFIK_ARCH=$TRAEFIK_ARCH \
    -t language-helper-bot .
docker tag language-helper-bot ghcr.io/konoplev/language-helper-bot:$TAG
docker push ghcr.io/konoplev/language-helper-bot:$TAG

docker tag sprach-partner ghcr.io/konoplev/language-helper-bot:latest
docker push ghcr.io/konoplev/language-helper-bot:latest


echo "ghcr.io/konoplev/language-helper-bot:$TAG"
#ssh pi@vpn.podcastov.net "sed -i -e 's@ghcr.io/konoplev/sprach-partner:.*@ghcr.io/konoplev/sprach-partner:$TAG@g' /mnt/ya/yd/projects/lingual.be/vocabulary/system/docker-compose.yaml; cd /mnt/ya/yd/projects/lingual.be/vocabulary/system; docker compose pull; docker compose down; docker compose up -d"
#ssh dietpi@vpn.podcastov.net "cd /home/dietpi/services && docker compose pull && docker compose down && docker compose  up -d"
