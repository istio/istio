package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "intern.go",
    ],
)

go_test(
    name = "intern_test",
    srcs = [ "intern_test.go" ],
    size = "small",
    library = ":go_default_library",
)
