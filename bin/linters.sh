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

echo 'Checking licenses'
bin/check_license.sh
echo 'licenses OK'

echo 'Installing gometalinter ....'
go get -u gopkg.in/alecthomas/gometalinter.v2
gometalinter=gometalinter.v2
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
