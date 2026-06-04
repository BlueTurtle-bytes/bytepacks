#!/bin/bash
set -e

go build -o bin/apexpack ./cmd/apexpack
./bin/apexpack build . --tag apexpack:latest

docker load < .apexpack-output/apexpack.tar

# apko tags the image with the arch suffix it used during the build.
# melangeArch() maps arm64→aarch64, everything else→amd64 — match that here.
case $(uname -m) in
  arm64|aarch64) ARCH_SUFFIX="aarch64" ;;
  *)             ARCH_SUFFIX="amd64"   ;;
esac
docker tag "apexpack:latest-${ARCH_SUFFIX}" apexpack:latest

docker save apexpack:latest -o /tmp/apexpack-latest.tar
kind load image-archive /tmp/apexpack-latest.tar --name kind-cluster
