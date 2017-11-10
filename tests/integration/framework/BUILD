load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "component.go",
        "environment.go",
        "framework.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//tests/util:go_default_library",
    ],
)
