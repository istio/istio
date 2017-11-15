workspace(name = "io_istio_api")

load("//:check_bazel_version.bzl", "check_version")
check_version()

git_repository(
    name = "io_bazel_rules_go",
    commit = "9cf23e2aab101f86e4f51d8c5e0f14c012c2161c",  # Oct 12, 2017 (Add `build_external` option to `go_repository`)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("//:api_dependencies.bzl", "mixer_api_dependencies")
mixer_api_dependencies()


