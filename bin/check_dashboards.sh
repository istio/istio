#!/bin/bash

SCRIPTPATH=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd --physical)
ROOTDIR=$(cd "$(dirname "${SCRIPTPATH}")" && pwd --physical)
pushd "$ROOTDIR"

if ! git diff --quiet addons/grafana/dashboards; then
    echo "Grafana dashboards have unstaged changes, please stage before linting."
    popd
    exit 1
fi

addons/grafana/fix_datasources.sh

if ! git diff --quiet addons/grafana/dashboards; then
    echo "Grafana dashboards' datasources fixed, please add to commit."
    popd
    exit 1
fi

popd
