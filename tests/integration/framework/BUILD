load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "framework.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//tests/integration/framework/component:go_default_library",
        "//tests/integration/framework/environment:go_default_library",
        "//tests/util:go_default_library",
    ],
)
