package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_prefix", "go_test")

go_prefix("istio.io/mixer")

go_library(
    name = "go_default_library",
    srcs = ["statsd.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/statsd2/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/pool:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_cactus_go_statsd_client//statsd:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["statsd_test.go"],
    library = ":go_default_library",
    deps = [
        "//adapter/statsd2/config:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_cactus_go_statsd_client//statsd:go_default_library",
        "@com_github_cactus_go_statsd_client//statsd/statsdtest:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)
