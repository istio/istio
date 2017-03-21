#!/usr/bin/env bash
WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

set -e
echo "Perf test"
DIRS="pkg/expr pkg/attribute"
cd $ROOT
for pkgdir in ${DIRS}; do
    cd ${ROOT}/${pkgdir} 
    go test -run=^$  -bench=.  -benchmem
done
