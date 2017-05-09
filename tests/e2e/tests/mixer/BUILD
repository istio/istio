load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_lib",
    testonly = 1,
    srcs = ["mixer_test.go"],
    deps = [
        "//tests/e2e/framework:go_default_library",
        "//tests/e2e/util:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_prometheus_client_model//go:go_default_library",
        "@com_github_prometheus_common//expfmt:go_default_library",
    ],
)

go_test(
    name = "large_tests",
    size = "large",
    srcs = ["mixer_test.go"],
    data = [
        "//demos/apps/bookinfo",
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
