#!/usr/bin/env bash

set -e

echo "Race test"
go test -race ./...
