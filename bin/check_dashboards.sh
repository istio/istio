#!/bin/bash
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
pushd $ROOTDIR

<<<<<<< HEAD
git diff --quiet addons/grafana/dashboards/
if [[ $? -ne 0 ]]; then
=======
if ! git diff --quiet install/kubernetes/helm/istio/charts/grafana/dashboards/; then
>>>>>>> 3f5810940... Use stock grafana images instead of custom docker build (#8189)
    echo "Grafana dashboards have unstaged changes, please stage before linting."
    popd
    exit 1
fi

install/kubernetes/helm/istio/charts/grafana/fix_datasources.sh

<<<<<<< HEAD
git diff --quiet addons/grafana/dashboards/
if [[ $? -ne 0 ]]; then
=======
if ! git diff --quiet install/kubernetes/helm/istio/charts/grafana/dashboards/; then
>>>>>>> 3f5810940... Use stock grafana images instead of custom docker build (#8189)
    echo "Grafana dashboards' datasources fixed, please add to commit."
    popd
    exit 1
fi

popd
