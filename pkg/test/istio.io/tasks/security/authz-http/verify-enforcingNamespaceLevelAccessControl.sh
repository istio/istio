#!/bin/bash

set -e
set -u
set -o pipefail

. ./verify-lib.sh

verify 200 "William Shakespeare" "Book Details" "Book Reviews"
