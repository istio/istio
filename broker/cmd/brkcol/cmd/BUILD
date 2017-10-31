package(default_visibility = ["//broker/cmd/brkcol:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["root.go"],
    visibility = ["//broker/cmd:__subpackages__"],
    deps = [
        "//broker/cmd/brks/cmd:go_default_library",
        "//broker/cmd/shared:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@com_github_spf13_cobra//doc:go_default_library",
    ],
)
