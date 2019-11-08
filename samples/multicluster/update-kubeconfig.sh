#!/bin/bash

set -e

: "${WORKDIR:?WORKDIR not set}"

: "${CLUSTER0_CONTEXT:?CLUSTER0_CONTEXT not set}"
: "${CLUSTER0_KUBECONFIG:?CLUSTER0_KUBECONFIG not set}"

: "${CLUSTER1_CONTEXT:?CLUSTER1_CONTEXT not set}"
: "${CLUSTER1_KUBECONFIG:?CLUSTER1_KUBECONFIG not set}"

: "${CLUSTER2_CONTEXT:?CLUSTER2_CONTEXT not set}"
: "${CLUSTER2_KUBECONFIG:?CLUSTER2_KUBECONFIG not set}"

if [[ ! -d ${WORKDIR} ]]; then
  echo "error: workspace directory '${WORKDIR}' does not exist"
  exit 1
fi

MERGED_KUBECONFIG=${WORKDIR}/"mesh.kubeconfig"

# create a local working directory to manage kubeconfig files.
export TMP_KUBECONFIG_DIR=$(mktemp -d ./kubeconfig_workdir_XXXXXX)
cleanup() {
  rm -rf ${TMP_KUBECONFIG_DIR}
}
trap cleanup EXIT

# track existing contexts that were already merged. non-unique contexts are
# renamed by appending NEXT_NUM_SUFFIX
declare -A EXISTING_CONTEXTS
NEXT_NUM_SUFFIX=1

# used to accumulate the merged kubeconfigs
export KUBECONFIG=""

function extract_context() {
  local IN_KUBECONFIG=${1}
  local IN_CONTEXT=${2}
  local EXISTING_CONTEXT=${@:3}

  echo "extracting ${IN_CONTEXT} from ${IN_KUBECONFIG}"

  local EXTRACTED_KUBECONFIG=$(mktemp ${TMP_KUBECONFIG_DIR}/XXXXXX)

  kubectl --kubeconfig ${IN_KUBECONFIG} --context ${IN_CONTEXT} \
    config view --minify --raw > ${EXTRACTED_KUBECONFIG}

  if [[ ! ${EXISTING_CONTEXT[${IN_CONTEXT}]+_} ]]
  then
    local CONTEXT_RENAMED=${IN_CONTEXT}"-${NEXT_NUM_SUFFIX}"
    NEXT_NUM_SUFFIX=$((NEXT_NUM_SUFFIX+1))

    echo "${IN_CONTEXT} will not be unique in the merged kubeconfig. Renaming to ${CONTEXT_RENAMED} "
    kubectl --kubeconfig ${EXTRACTED_KUBECONFIG} \
      config rename-context ${IN_CONTEXT} ${CONTEXT_RENAMED} > /dev/null
    IN_CONTEXT=${CONTEXT_RENAMED}
  fi

  export KUBECONFIG=${KUBECONFIG}:${EXTRACTED_KUBECONFIG}

  EXISTING_CONTEXTS["${IN_CONTEXT}"]=y
}

extract_context ${CLUSTER0_KUBECONFIG} ${CLUSTER0_CONTEXT}
extract_context ${CLUSTER1_KUBECONFIG} ${CLUSTER1_CONTEXT}
extract_context ${CLUSTER2_KUBECONFIG} ${CLUSTER2_CONTEXT}

echo "creating ${MERGED_KUBECONFIG} from extracted contexts"
KUBECONFIG=${KUBECONFIG} kubectl config view --raw --flatten > ${MERGED_KUBECONFIG}

echo ""
echo "Success! Run the following command to use as the default kubeconfig in the current shell"
echo
echo "    export KUBECONFIG=${MERGED_KUBECONFIG}"
echo
