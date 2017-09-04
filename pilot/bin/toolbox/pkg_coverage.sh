bazel build @com_github_istio_test_infra//toolbox/pkg_check \
  || { echo 'Failed to build pkg_check'; exit 0; }
bazel-bin/external/com_github_istio_test_infra/toolbox/pkg_check/pkg_check
