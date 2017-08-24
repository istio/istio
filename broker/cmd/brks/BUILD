package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_binary(
    name = "brks",
    library = ":go_default_library",
    linkstamp = "istio.io/broker/pkg/version",
)

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    deps = ["//cmd/brks/cmd:go_default_library"],
)
