#!/bin/bash

set -e

THIS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

for db in ${THIS_DIR}/dashboards/*.json; do
    sed -i 's/${DS_PROMETHEUS}/Prometheus/g' $db
done
