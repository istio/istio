#!/bin/bash
cd  "$( dirname "${BASH_SOURCE[0]}" )"

if [ -z $PIPELINE ] ; then
    PIPELINE=istio-installer
fi
if [ -z $TARGET ] ; then
    TARGET=concourse-sapcloud
fi

if [ -n "$1" ] ; then
    fly --target $TARGET login --concourse-url $1
fi

fly -t $TARGET set-pipeline -c concourse.yaml -p $PIPELINE