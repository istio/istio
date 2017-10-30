# TODO: Convert to a gazelle friendly go_binary.name and remove "ignore"
# gazelle:ignore

package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    deps = [
        "//adapter:go_default_library",
        "//cmd/server/cmd:go_default_library",
        "//cmd/shared:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/template:go_default_library",
        "//template:go_default_library",
    ],
)

go_binary(
    name = "mixs",
    library = ":go_default_library",
)
