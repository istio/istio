package(default_visibility = ["//broker/cmd/brks:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_binary(
    name = "brks",
    library = ":go_default_library",
    linkstamp = "istio.io/istio/broker/pkg/version",
    visibility = ["//visibility:public"],
)

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    deps = ["//broker/cmd/brks/cmd:go_default_library"],
)
