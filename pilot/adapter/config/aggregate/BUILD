load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["config.go"],
    visibility = ["//visibility:public"],
    deps = ["//model:go_default_library"],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = ["config_test.go"],
    deps = [
        ":go_default_library",
        "//adapter/config/memory:go_default_library",
        "//model:go_default_library",
        "//test/mock:go_default_library",
    ],
)
