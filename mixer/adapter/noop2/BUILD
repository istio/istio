package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["noop.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/handler:go_default_library",
        "//template/checknothing:go_default_library",
        "//template/listentry:go_default_library",
        "//template/logentry:go_default_library",
        "//template/metric:go_default_library",
        "//template/quota:go_default_library",
        "//template/reportnothing:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["noop_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "//template/checknothing:go_default_library",
        "//template/listentry:go_default_library",
        "//template/logentry:go_default_library",
        "//template/metric:go_default_library",
        "//template/quota:go_default_library",
        "//template/reportnothing:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
