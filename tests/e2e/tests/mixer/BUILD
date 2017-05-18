load("@io_bazel_rules_go//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    size = "large",
    srcs = ["mixer_test.go"],
    data = [
        "//samples/apps/bookinfo",
    ],
    tags = ["manual"],
    deps = [
        "//tests/e2e/framework:go_default_library",
        "//tests/e2e/util:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_prometheus_client_model//go:go_default_library",
        "@com_github_prometheus_common//expfmt:go_default_library",
    ],
)
