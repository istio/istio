package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test", "go_binary")

go_binary(
    name = "mixgenproc",
    srcs = [
        "main.go",
    ],
    deps = [
        "//tools/codegen/pkg/interfacegen:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
    ],
)
