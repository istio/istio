#!/usr/bin/env bash

set -euo pipefail

# testEnv will setup a local test environment, for running Istio unit tests.

# Based on circleCI config - used to reproduce the environment and to improve local testing

# expect istio scripts to be under $GOPATH/src/istio.io/istio/bin/...
export TOP=$(cd $(dirname $0)/../../../..; pwd)
export ISTIO_GO=${TOP}/src/istio.io/istio

export GOPATH=${TOP}
export PATH=${GOPATH}/bin:${PATH}
export OUT=${TOP}/out

# components used in the test (starting with circleci for consistency, eventually ci will use this)
export K8S_VER=v1.7.4
export MASTER_IP=127.0.0.1
export MASTER_CLUSTER_IP=10.99.0.1
export ETCD_VER=v3.2.15

# TODO: customize the ports and generate a local config
export KUBECONFIG=${TOP}/src/istio.io/istio/.circleci/config

export USE_BAZEL=0

${ISTIO_GO}/bin/init.sh

CERTDIR=${CERTDIR:-${OUT}/istio-certs}
LOG_DIR=${LOG_DIR:-${OUT}/log}
ETCD_DATADIR=${ETCD_DATADIR:-${OUT}/etcd-data}

EASYRSA_DIR=$OUT/easy-rsa-master/easyrsa3
EASYRSA=$EASYRSA_DIR/easyrsa

# Ensure k8s certificats - if not found, download easy-rsa and create k8s certs
function ensureK8SCerts() {
    if [ -f ${CERTDIR}/apiserver.key ] ; then
        return
    fi

    mkdir -p ${CERTDIR}
    pushd $OUT
    curl -L -O https://storage.googleapis.com/kubernetes-release/easy-rsa/easy-rsa.tar.gz
    tar xzf easy-rsa.tar.gz
    cd easy-rsa-master/easyrsa3

    ./easyrsa init-pki > /dev/null
    ./easyrsa --batch "--req-cn=${MASTER_IP}@`date +%s`" build-ca nopass > /dev/null
    ./easyrsa --subject-alt-name="IP:${MASTER_IP},""IP:${MASTER_CLUSTER_IP},""DNS:kubernetes,""DNS:kubernetes.default,""DNS:kubernetes.default.svc,""DNS:kubernetes.default.svc.cluster,""DNS:kubernetes.default.svc.cluster.local" --days=10000 build-server-full server nopass > /dev/null

    cp pki/ca.crt ${CERTDIR}/k8sca.crt
    cp pki/issued/server.crt ${CERTDIR}/apiserver.crt
    cp pki/private/server.key ${CERTDIR}/apiserver.key
    popd
}

# Get dependencies needed for tests. Only needed once.
# The docker builder image should include them.
function getDeps() {
   mkdir -p $TOP/bin
   if [ ! -f $TOP/bin/kubectl ] ; then
     curl -Lo $TOP/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kubectl && chmod +x $TOP/bin/kubectl
   fi
   if [ ! -f $TOP/bin/kube-apiserver ] ; then
     curl -Lo ${TOP}/bin/kube-apiserver https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kube-apiserver && chmod +x ${TOP}/bin/kube-apiserver
   fi
   if [ ! -f $TOP/bin/etcd ] ; then
     curl -L https://github.com/coreos/etcd/releases/download/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz | tar xz -O etcd-${ETCD_VER}-linux-amd64/etcd > ${TOP}/bin/etcd && chmod +x ${TOP}/bin/etcd
   fi
   if [ ! -f $TOP/bin/envoy ] ; then
     # Init should be run after cloning the workspace
     ${ISTIO_GO}/bin/init.sh
   fi
}

# No root required, run local etcd and kube apiserver for tests.
function startLocalApiserver() {
    ensureK8SCerts
    getDeps

    mkdir -p ${LOG_DIR}
    mkdir -p ${ETCD_DATADIR}
    ${TOP}/bin/etcd --data-dir ${ETCD_DATADIR} > ${LOG_DIR}/etcd.log 2>&1 &
    echo $! > $LOG_DIR/etcd.pid

    ${TOP}/bin/kube-apiserver --etcd-servers http://127.0.0.1:2379 \
        --client-ca-file ${CERTDIR}/k8sca.crt \
        --requestheader-client-ca-file ${CERTDIR}/k8sca.crt \
        --tls-cert-file ${CERTDIR}/apiserver.crt \
        --tls-private-key-file ${CERTDIR}/apiserver.key \
        --service-cluster-ip-range 10.99.0.0/16 \
        --port 8080 -v 2 --insecure-bind-address 0.0.0.0 \
        > ${LOG_DIR}/apiserver.log 2>&1 &
    echo $! > $LOG_DIR/apiserver.pid

    echo "Started local etcd and apiserver !"
}

function stopLocalApiserver() {
  if [[ -f $LOG_DIR/etcd.pid ]] ; then
    kill -9 $(cat $LOG_DIR/etcd.pid)
    kill -9 $(cat $LOG_DIR/apiserver.pid)
    rm $LOG_DIR/{etcd,apiserver}.pid
  fi
}

function ensureLocalApiserver() {
    kubectl get nodes 2>/dev/null || startLocalApiserver
}

case "$1" in
    start) startLocalApiserver ;;
    stop) stopLocalApiserver ;;
    ensure) ensureLocalApiserver ;;
esac
