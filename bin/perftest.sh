#!/usr/bin/env bash
set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOT=$SCRIPTPATH/..
cd $ROOT


echo "Perf test"
DIRS="mixer/pkg/api mixer/pkg/cache mixer/pkg/expr mixer/pkg/il/interpreter"
cd $ROOT
for pkgdir in ${DIRS}; do
    cd ${ROOT}/${pkgdir} 
    go test -run=^$  -bench=.  -benchmem
done
