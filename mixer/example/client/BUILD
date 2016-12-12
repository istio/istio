package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary")

go_binary(
    name = "client",
    srcs = [
        "client.go",
    ],
    deps = [
        "//example/client/cmd:go_default_library",
    ],
)
