load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["client.go"],
    visibility = ["//visibility:private"],
    deps = ["@com_github_spf13_pflag//:go_default_library"],
)

go_binary(
    name = "client",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
