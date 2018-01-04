#!/usr/bin/env bash

pushd $GOPATH/src/istio.io/istio/vendor/github.com/golang/mock/mockgen
go build
popd

$GOPATH/src/istio.io/istio/vendor/github.com/golang/mock/mockgen/mockgen -source store.go -destination mock_store_test.go -package config