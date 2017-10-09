package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

alias(
    name = "mixcol",
    actual = ":collateral",
)

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    deps = [
        "//cmd/collateral/cmd:go_default_library",
        "//cmd/shared:go_default_library",
    ],
)

go_binary(
    name = "collateral",
    library = ":go_default_library",
)
