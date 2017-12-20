# TODO: Convert to a gazelle friendly go_binary.name and remove "ignore"
# gazelle:ignore

package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    testonly = 1,
    srcs = ["main.go"],
    deps = [
        "//mixer/pkg/perf:go_default_library",
    ],
)

go_binary(
    name = "perfclient",
    testonly = 1,
    library = ":go_default_library",
)
