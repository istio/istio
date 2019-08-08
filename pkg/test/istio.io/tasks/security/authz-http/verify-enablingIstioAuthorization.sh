#!/bin/bash

set -e
set -u
set -o pipefail

. ./verify-lib.sh

verify 403 "RBAC: access denied"
