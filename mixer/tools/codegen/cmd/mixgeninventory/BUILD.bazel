load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//tools/codegen/pkg/inventory:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
    ],
)

go_binary(
    name = "mixgeninventory",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
