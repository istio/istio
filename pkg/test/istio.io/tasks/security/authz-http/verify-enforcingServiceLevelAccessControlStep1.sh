#!/bin/bash

set -e
set -u
set -o pipefail

. ./verify-lib.sh

verify 200 "William Shakespeare" "Error fetching product details" "Error fetching product reviews"
