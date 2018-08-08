#!/bin/bash
set -ex

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

WORKSPACE=$SCRIPTPATH/..

cd ${WORKSPACE}

if [[ -z $SKIP_INIT ]];then
  bin/init.sh
fi

echo 'Checking Pilot types generation ....'
bin/check_pilot_codegen.sh
echo 'Pilot types generation OK'

echo 'Running format/imports check ....'
bin/fmt.sh -c
echo 'Format/imports check OK'

echo 'Checking licenses'
bin/check_license.sh
echo 'licenses OK'

echo 'Installing gometalinter ....'
go get -u gopkg.in/alecthomas/gometalinter.v2
gometalinter=$(command -v gometalinter.v2 2> /dev/null || echo "${ISTIO_BIN}/gometalinter.v2")
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

echo 'Running testlinter ...'
pushd tests/util/checker/testlinter
go install .
popd

$gometalinter --config=./tests/util/checker/testlinter/testlinter.json ./...
echo 'testlinter OK'

echo 'Running helm lint on istio & istio-remote ....'
helm lint ./install/kubernetes/helm/{istio,istio-remote}
echo 'helm lint on istio & istio-remote OK'

echo 'Checking Grafana dashboards'
bin/check_dashboards.sh
echo 'dashboards OK'
