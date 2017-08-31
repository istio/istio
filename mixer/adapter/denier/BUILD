package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["denier.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/denier/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/handler:go_default_library",
        "//template/checknothing:go_default_library",
        "//template/listentry:go_default_library",
        "//template/quota:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["denier_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "//template/checknothing:go_default_library",
        "//template/listentry:go_default_library",
        "//template/quota:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
