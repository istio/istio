#!/bin/bash

# Run all tests in the sub-folders
# Somehow it fails to run as "go test ./..."
# Mock mixer server failed to bind its listening port.

for f in $(find $(dirname $0) -name *_test.go); do echo "go test $f"; go test $f;  done
