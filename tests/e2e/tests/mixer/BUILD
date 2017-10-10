load("@io_bazel_rules_go//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    size = "large",
    srcs = ["mixer_test.go"],
    data = [
        "//samples/bookinfo",
        "//tests/e2e/tests:testdata",
    ],
    tags = ["manual"],
    deps = [
        "//tests/e2e/framework:go_default_library",
        "//tests/e2e/util:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_prometheus_client_golang//api:go_default_library",
        "@com_github_prometheus_client_golang//api/prometheus/v1:go_default_library",
        "@com_github_prometheus_common//model:go_default_library",
        "@io_istio_fortio//http:go_default_library",
    ],
)
