#!/bin/bash

set -e
set -u
set -o pipefail

. ./verify-lib.sh

# TODO(yangminzhu): Verify the rating page in the response.
verify 200 "William Shakespeare" "Book Details" "Book Reviews"
