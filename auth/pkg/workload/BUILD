load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "secretfileserver.go",
        "secretserver.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/util:go_default_library",
    ],
)
