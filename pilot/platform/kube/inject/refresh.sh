#!/bin/bash

workspace=$(bazel info workspace)
for f in ${workspace}/platform/kube/inject/testdata/*.yaml; do
  bazel run //cmd/istioctl:istioctl -- kube-inject -f ${f} -o ${f}.injected --setVersionString 12345678
done
