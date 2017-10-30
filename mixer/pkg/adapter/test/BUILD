package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    testonly = 1,
    srcs = [
        "adapters.go",
        "env.go",
    ],
    visibility = ["//visibility:public"],
    deps = ["//pkg/adapter:go_default_library"],
)
