package(default_visibility = ["//example/servicegraph:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_binary")

go_library(
    name = "go_default_library",
    srcs = [
        "main.go",
    ],
    deps = [
        "//example/servicegraph:go_default_library",
        "//example/servicegraph/dot:go_default_library",
        "//example/servicegraph/promgen:go_default_library",
    ],
)

go_binary(
    name = "servicegraph",
    data = ["//example/servicegraph/js:viz/viz-lite.js"],
    library = ":go_default_library",
)
