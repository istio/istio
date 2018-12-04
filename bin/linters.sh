#!/bin/bash
set -ex

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

WORKSPACE=$SCRIPTPATH/..

cd ${WORKSPACE}

GOOS=
GOARCH=

if [[ -z $SKIP_INIT ]];then
  bin/init.sh
fi

echo 'Checking Pilot types generation ....'
bin/check_pilot_codegen.sh
echo 'Pilot types generation OK'

echo 'Skipping format/imports check ....'
# FIXME: goimports is broken and go get doesn't let you pick a version
# Turn it off for now to avoid spurious whitenoise changes that will make
# merges hard.
# https://github.com/golang/go/issues/28200
# https://github.com/istio/istio/pull/9526
#echo 'Running format/imports check ....'
#bin/fmt.sh -c
#echo 'Format/imports check OK'

echo 'Checking licenses'
bin/check_license.sh
echo 'licenses OK'

echo 'Installing gometalinter ....'
go get -u gopkg.in/alecthomas/gometalinter.v2
gometalinter=$(which gometalinter.v2 2> /dev/null || echo "${ISTIO_BIN}/gometalinter.v2")
$gometalinter --install
echo 'Gometalinter installed successfully ....'

echo 'Running gometalinter ....'
$gometalinter --config=./lintconfig_base.json ./...
echo 'gometalinter OK'

echo 'Running gometalinter on adapters ....'
pushd mixer/tools/adapterlinter
go install .
popd

$gometalinter --config=./mixer/tools/adapterlinter/gometalinter.json ./mixer/adapter/...
echo 'gometalinter on adapters OK'

echo 'Running helm lint on istio & istio-remote ....'
helm lint ./install/kubernetes/helm/{istio,istio-remote}
echo 'helm lint on istio & istio-remote OK'

echo 'Checking Grafana dashboards'
bin/check_dashboards.sh
echo 'dashboards OK'
