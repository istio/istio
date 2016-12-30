load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["istioctl.go"],
    visibility = ["//visibility:private"],
    deps = ["//cli:go_default_library"],
)

go_binary(
    name = "main",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
