package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary")

go_binary(
    name = "brks",
    srcs = [
        "main.go",
    ],
    linkstamp = "istio.io/broker/pkg/version",
    deps = [
        "//cmd/server/cmd:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
    ],
)
