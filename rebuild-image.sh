#!/bin/bash
set -e

go build -o bin/apexpack ./cmd/apexpack
./bin/apexpack build . --tag apexpack:latest

docker load < .apexpack-output/apexpack.tar
docker tag apexpack:latest-amd64 apexpack:latest
docker save apexpack:latest -o /tmp/apexpack-latest.tar
kind load image-archive /tmp/apexpack-latest.tar --name kind-cluster
