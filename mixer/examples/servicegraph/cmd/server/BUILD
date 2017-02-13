package(default_visibility = ["//visibility:private"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_binary")

go_library(
    name = "go_default_library",
    srcs = [
        "main.go",
    ],
    deps = [
        "//examples/servicegraph:go_default_library",
        "//examples/servicegraph/dot:go_default_library",
        "//examples/servicegraph/promgen:go_default_library",
    ],
)

go_binary(
    name = "servicegraph",
    data = ["//examples/servicegraph:js/viz-lite.js"],
    library = ":go_default_library",
)
