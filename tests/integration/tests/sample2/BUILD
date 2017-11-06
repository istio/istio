load("@io_bazel_rules_go//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    srcs = ["sample2_test.go"],
    deps = [
        "//tests/integration/framework/environment:go_default_library",
        "//tests/integration/framework:go_default_library",
        "//tests/util:go_default_library",
        "@com_github_prometheus_client_golang//api:go_default_library",
        "@com_github_prometheus_client_golang//api/prometheus/v1:go_default_library",
        "@com_github_prometheus_common//model:go_default_library",
    ],
)
