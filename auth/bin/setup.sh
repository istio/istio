#!/bin/bash

set -ex

# Create symlinks from bazel directory to the vendor folder.
bin/bazel_to_go.py

# Remove nested vendor/ directories.
find -L vendor/ -mindepth 1 -name vendor | xargs rm -rf

go install ./...
