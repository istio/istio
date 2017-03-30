#!/bin/bash

workspace=$(bazel info workspace)
for f in ${workspace}/platform/kube/inject/testdata/*.yaml; do
    coreDump=false
    if [ "$(basename ${f})" == "enable-core-dump.yaml" ]; then
	coreDump=true
    fi
    bazel run //cmd/istioctl:istioctl -- \
          kube-inject -f ${f} -o ${f}.injected --setVersionString 12345678 --tag unittest --coreDump=${coreDump}
done
