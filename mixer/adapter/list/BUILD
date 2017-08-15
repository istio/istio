package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "ipList.go",
        "list.go",
        "stringList.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/list/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//template/listentry:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["list_test.go"],
    library = ":go_default_library",
    deps = [
        "//adapter/list/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
    ],
)
