#!/bin/bash

set -ex

# Create symlinks from bazel directory to the vendor folder.
bin/bazel_to_go.py

# Link generated proto files
ln -sf $(bazel info bazel-genfiles)/proto/ca_service.pb.go proto/


mkdir -p vendor/github.com/googleapis/googleapis/google/rpc
for f in $(bazel info bazel-genfiles)/external/com_github_googleapis_googleapis/google/rpc/*.pb.go; do
  ln -sf $f vendor/github.com/googleapis/googleapis/google/rpc/
done

# Remove nested vendor/ directories.
find -L vendor/ -mindepth 1 -name vendor | xargs rm -rf

go install ./...
