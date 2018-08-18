#!/bin/bash

set -e

THIS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
UX=$(uname)

for db in "${THIS_DIR}"/dashboards/*.json; do
    if [[ ${UX} == "Darwin" ]]; then
        # shellcheck disable=SC2016
        sed -i '' 's/${DS_PROMETHEUS}/Prometheus/g' "$db"
    else
        # shellcheck disable=SC2016
        sed -i 's/${DS_PROMETHEUS}/Prometheus/g' "$db"
    fi
done
