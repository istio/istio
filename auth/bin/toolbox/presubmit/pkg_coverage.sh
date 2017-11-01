
#!/bin/bash
set -ex

bazel build @com_github_istio_test_infra//toolbox/pkg_check
bazel-bin/external/com_github_istio_test_infra/toolbox/pkg_check/pkg_check
