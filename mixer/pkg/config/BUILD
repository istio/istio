package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "inventory.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/config/crd:go_default_library",
        "//mixer/pkg/config/store:go_default_library",
    ],
)
