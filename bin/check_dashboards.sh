#!/bin/bash
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
pushd $ROOTDIR

if ! git diff --quiet install/kubernetes/helm/istio/charts/grafana/dashboards/; then
    echo "Grafana dashboards have unstaged changes, please stage before linting."
    popd
    exit 1
fi

install/kubernetes/helm/istio/charts/grafana/fix_datasources.sh

if ! git diff --quiet install/kubernetes/helm/istio/charts/grafana/dashboards/; then
    echo "Grafana dashboards' datasources fixed, please add to commit."
    popd
    exit 1
fi

popd
