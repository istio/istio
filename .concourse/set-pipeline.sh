#!/bin/bash

if [ -z $PIPELINE ] ; then
    PIPELINE=istio-installer
fi
if [ -z $TARGET ] ; then
    TARGET=concourse_ci
fi
if [ -z $KUBECONFIG ] ; then
    echo "KUBECONFIG has to be set"
    exit 1
fi

if [ -n "$1" ] ; then
    fly --target $TARGET login --concourse-url $1
fi

fly -t $TARGET set-pipeline -c concourse.yaml -p $PIPELINE -v KUBECONFIG="$(cat $KUBECONFIG)"