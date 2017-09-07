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
        "check.go",
        "configError.go",
        "denials.go",
        "handler.go",
        "info.go",
        "labels.go",
        "lists.go",
        "metrics.go",
        "quotas.go",
        "registrar.go",
        "result.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/status:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        # "//template/checknothing:go_default_library_gogo_proto",
        # "//template/listentry:go_default_library_gogo_proto",
        # "//template/logentry:go_default_library_gogo_proto",
        # "//template/metric:go_default_library_gogo_proto",
        # "//template/quota:go_default_library_gogo_proto",
        # "//template/reportnothing:go_default_library_gogo_proto",
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
        "result_test.go",
    ],
    library = ":go_default_library",
    deps = ["@com_github_googleapis_googleapis//:google/rpc"],
)
