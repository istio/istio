load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "mixer_binary_comp.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//tests/integration/framework:go_default_library",
        "//tests/util:go_default_library",
    ],
)
