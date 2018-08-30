#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

go run "${DIR}/main.go" "${DIR}/gen-metadata-defs" "${1}" "${DIR}/metadata.yaml" "${DIR}/../../${2}"
goimports -w -local istio.io "${DIR}/../../${2}"
