#!/usr/bin/env bash
set -e
echo "" > coverage.out

for d in $(go list ./pkg/...); do
	go test -race -coverprofile=profile.out -covermode=atomic $d
	if [ -f profile.out ]; then
		cat profile.out >> coverage.out
		rm profile.out
	fi
done
