#!/bin/bash
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
pushd $ROOTDIR

git diff --quiet addons/grafana/dashboards/
if [[ $? -ne 0 ]]; then
    echo "Grafana dashboards have unstaged changes, please stage before linting."
    popd
    exit 1
fi

addons/grafana/fix_datasources.sh

git diff --quiet addons/grafana/dashboards/
if [[ $? -ne 0 ]]; then
    echo "Grafana dashboards' datasources fixed, please add to commit."
    popd
    exit 1
fi

popd
