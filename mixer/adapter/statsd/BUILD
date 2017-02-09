package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "statsd.go",
    ],
    deps = [
        "//adapter/statsd/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "@com_github_cactus_statsd_client//statsd:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = ["statsd_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter/test:go_default_library",
        "@com_github_cactus_statsd_client//statsd/statsdtest:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
