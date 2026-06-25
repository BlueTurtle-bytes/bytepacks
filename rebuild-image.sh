#!/bin/bash
set -e

go build -o bin/apexpacks ./cmd/apexpacks
./bin/apexpacks build . --tag apexpack:latest --project-name apexpacks

docker load < .apexpack-output/apexpack.tar

# Docker normalises the arch name when loading an OCI tar:
#   apko internal name  →  Docker tag suffix
#   aarch64             →  arm64
#   x86_64              →  amd64
case $(uname -m) in
  arm64|aarch64) ARCH_SUFFIX="arm64" ;;
  *)             ARCH_SUFFIX="amd64" ;;
esac
docker tag "apexpack:latest-${ARCH_SUFFIX}" apexpack:latest

docker save apexpack:latest -o /tmp/apexpack-latest.tar
kind load image-archive /tmp/apexpack-latest.tar --name dev-local
