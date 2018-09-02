#!/bin/bash
# Our goal is to build a smallest image of adapter. So that we need to specify CGO_ENABLED for static linking
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o nristioadapter
cp nristioadapter docker/
docker build docker/ -t wentaozhang/nradapter:0.1.x
docker push wentaozhang/nradapter:0.1.x