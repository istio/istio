package(default_visibility = ["//mixer/example/servicegraph/cmd:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["dot.go"],
    visibility = ["//visibility:public"],
    deps = ["//mixer/example/servicegraph:go_default_library"],
)
