package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "adapter.go",
        "builder.go",
    ],
    deps = [
        "//adapters:go_default_library",
    ],
)

go_test(
    name = "json_logger_test",
    srcs = [ "jsonLogger_test.go" ],
    size = "small",
    deps = [ "//adapters/testutil:go_default_library" ],
    library = ":go_default_library",
)
