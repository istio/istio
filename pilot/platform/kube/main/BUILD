load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["kube_agent.go"],
    visibility = ["//visibility:private"],
)

go_binary(
    name = "main",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
