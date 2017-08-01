# gazelle:ignore
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_binary(
    name = "mixcol",
    srcs = [
        "main.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//cmd/collateral/cmd:go_default_library",
        "//cmd/shared:go_default_library",
    ],
)
