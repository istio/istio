#!/usr/bin/env bash

ROOT=$GOPATH/src/istio.io/api

sed -n '/gogo\/protobuf/,/\[\[projects/p' $ROOT/Gopkg.lock | grep "version = " | sed -e 's/^[^\"]*\"//g' -e 's/\"//g'