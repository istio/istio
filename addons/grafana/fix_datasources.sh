#!/bin/bash
# shellcheck disable=SC2016

set -e

THIS_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
UX=$(uname)

for db in ${THIS_DIR}/dashboards/*.json; do
    if [[ ${UX} == "Darwin" ]]; then
      sed -i '' 's/${DS_PROMETHEUS}/Prometheus/g' "$db"
    else
      sed -i 's/${DS_PROMETHEUS}/Prometheus/g' "$db"
    fi
done
