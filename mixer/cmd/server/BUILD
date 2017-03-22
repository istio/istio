package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_binary(
    name = "mixs",
    srcs = [
        "main.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//cmd/server/cmd:go_default_library",
    ],
)
