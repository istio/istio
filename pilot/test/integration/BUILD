load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_binary")

go_library(
    name = "go_default_library",
    srcs = ["templater.go"],
    visibility = ["//visibility:private"],
    deps = ["@in_gopkg_yaml_v2//:go_default_library"],
)

go_binary(
    name = "integration",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
