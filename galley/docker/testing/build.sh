#!/bin/bash

#
# To test/run the image manually, use this command-line:
# docker run -it --mount type=tmpfs,target=/app,tmpfs-mode=1770 --expose=8080  -p 8080:8080 <imageid>

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

SCRATCH_DIR="/tmp/galley/test/docker"

K8S_VER="${K8S_VER:-v1.9.2}"
ETCD_VER="${ETCD_VER:-v3.2.15}"

# The tag is used to keep a consistent set of images, aligned with code-changes.
TAG="v1"

# TODO: A better location to publish the images.
HUB="docker.io/ozevren/galley-testing"

rm -rf "${SCRATCH_DIR}"
mkdir -p "${SCRATCH_DIR}"
mkdir "${SCRATCH_DIR}/bin"

curl -Lo ${SCRATCH_DIR}/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${K8S_VER}/bin/linux/amd64/kubectl && chmod +x ${SCRATCH_DIR}/bin/kubectl
curl -Lo ${SCRATCH_DIR}/bin/kube-apiserver https://storage.googleapis.com/kubernetes-release/release/${K8S_VER}/bin/linux/amd64/kube-apiserver && chmod +x ${SCRATCH_DIR}/bin/kube-apiserver
curl -L https://github.com/coreos/etcd/releases/download/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz | tar xz -O etcd-${ETCD_VER}-linux-amd64/etcd > ${SCRATCH_DIR}/bin/etcd && chmod +x ${SCRATCH_DIR}/bin/etcd

cp "${SCRIPT_DIR}/Dockerfile" "${SCRATCH_DIR}/Dockerfile"
cp "${SCRIPT_DIR}/init.sh" "${SCRATCH_DIR}/init.sh"

docker build "${SCRATCH_DIR}" --tag "${TAG}"

docker image push ${HUB}:${TAG}
