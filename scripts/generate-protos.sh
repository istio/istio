#!/bin/bash

set -o errexit
set -o nounset

make clean generate

bazel build //...

genfiles=$(bazel info bazel-genfiles)
files=$(find -L ${genfiles} -name "*.pb.go")

for src in ${files}; do
    dst=${src##${genfiles}/}
    if [[ "${dst}" == "mixer*" ]]; then
        continue
    fi
    echo "copying $src to $dst ..."
    if [ -d "$(dirname ${dst})" ]; then
        install -m 0640 ${src} ${dst}
    fi
done

if [ -e mixer/v1/config/cfg.pb.go ]; then
    rm mixer/v1/config/cfg.pb.go
fi 
