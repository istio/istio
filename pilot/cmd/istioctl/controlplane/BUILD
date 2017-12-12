load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "cmd.go",
        "helm.go",
    ],
    visibility = ["//pilot/cmd/istioctl:__subpackages__"],
    deps = ["@com_github_spf13_cobra//:go_default_library"],
)
