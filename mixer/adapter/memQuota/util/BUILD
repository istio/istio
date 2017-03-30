package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "util.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/pool:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
