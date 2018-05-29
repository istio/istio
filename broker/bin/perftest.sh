#!/bin/bash
set -e
SCRIPTPATH=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd --physical)
source $SCRIPTPATH/use_bazel_go.sh

ROOT=$(cd "$(dirname "${SCRIPTPATH}")" && pwd --physical)
cd $ROOT


echo "Perf test"
DIRS=""
cd $ROOT
for pkgdir in ${DIRS}; do
  cd ${ROOT}/${pkgdir}
  go test -run=^$  -bench=.  -benchmem
done
