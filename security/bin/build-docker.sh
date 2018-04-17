#!/bin/bash
## Build the docker image for nodeagent (default)
## Invoke it from the base directory of the repo

usage() {
  [[ -n "${1}" ]] && echo "${1}"

  cat <<EOF
usage: ${BASH_SOURCE[0]} [options ...]"
  options::
   -c ... do a clean build
   -t ... tag to use
   -i ... image to build
EOF
  exit 2
}

ROOT="$(pwd)"
IMAGE="driver"
REG="gcr.io/kubernetes-1-151323"
TAG="latest"

CLEAN_BUILD=0
while getopts ct:i:r: arg; do
  case ${arg} in
     c) CLEAN_BUILD=1 ;;
     t) TAG="${OPTARG}" ;;
     i) IMAGE="${OPTARG}" ;;
     r) REG="${OPTARG}" ;;
     *) usage "Invalid option: -${OPTARG}" ;;
  esac
done

DEBUG_IMAGE_NAME="${REG}/${IMAGE}:${TAG}"
TARGET_DIR="${ROOT}/bin/${IMAGE}"

rm -rf ${TARGET_DIR}
go build cmd/node_agent_k8s/flexvolume/main.go

OPFILE="main"
hostdir=${ROOT}

if [ ! -f ${OPFILE} ]; then
   echo "No file ${OPFILE}"
   exit 2
fi

mkdir -p ${TARGET_DIR}

cp ${OPFILE} ${TARGET_DIR}/${IMAGE}
cp ${hostdir}/bin/${IMAGE}.sh ${TARGET_DIR}/
cp docker/Dockerfile.node-agent-k8s ${TARGET_DIR}/
docker build -f ${TARGET_DIR}/Dockerfile.node-agent-k8s -t "${DEBUG_IMAGE_NAME}" ${TARGET_DIR}
echo "Push ${DEBUG_IMAGE_NAME} to a registry now"
gcloud docker -- push "${DEBUG_IMAGE_NAME}"
