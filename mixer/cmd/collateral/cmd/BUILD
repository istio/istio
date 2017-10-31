package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["root.go"],
    visibility = ["//mixer/cmd:__subpackages__"],
    deps = [
        "//mixer/cmd/client/cmd:go_default_library",
        "//mixer/cmd/server/cmd:go_default_library",
        "//mixer/cmd/shared:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@com_github_spf13_cobra//doc:go_default_library",
    ],
)
