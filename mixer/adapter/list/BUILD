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
        "//mixer/adapter/list/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/listentry:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["list_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter/list/config:go_default_library",
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/template/listentry:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
