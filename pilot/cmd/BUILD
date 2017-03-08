load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["root.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//cmd/version:go_default_library",
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
    ],
)
