package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "accessLogs.go",
        "adapter.go",
        "applicationLogs.go",
        "attributes.go",
        "builder.go",
        "cacheability.go",
        "configError.go",
        "denials.go",
        "handler.go",
        "info.go",
        "labels.go",
        "lists.go",
        "metrics.go",
        "quotas.go",
        "registrar.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "applicationLogs_test.go",
        "builder_test.go",
        "configError_test.go",
        "metrics_test.go",
    ],
    library = ":go_default_library",
    deps = ["@com_github_googleapis_googleapis//:google/rpc"],
)
