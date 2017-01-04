load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "agent.go",
        "config.go",
        "resources.go",
    ],
    visibility = ["//visibility:public"],
    deps = ["//model:go_default_library"],
)

go_test(
    name = "go_default_test",
    srcs = ["config_test.go"],
    library = ":go_default_library",
)
