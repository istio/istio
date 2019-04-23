workspace(name = "io_istio_api")

load("//:check_bazel_version.bzl", "check_version")
check_version()

# Oct 12, 2017 (Add `build_external` option to `go_repository`)
RULES_GO_SHA = "9cf23e2aab101f86e4f51d8c5e0f14c012c2161c"
RULES_GO_SHA256 = "76133849005134eceba9080ee28cef03316fd29f64a0a8a3ae09cd8862531d15"

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
http_archive(
    name = "io_bazel_rules_go",
    strip_prefix = "rules_go-" + RULES_GO_SHA,
    url = "https://github.com/bazelbuild/rules_go/archive/" + RULES_GO_SHA + ".tar.gz",
    sha256 = RULES_GO_SHA256,
)

load("//:api_dependencies.bzl", "mixer_api_dependencies")
mixer_api_dependencies()
