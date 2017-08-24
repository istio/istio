load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["version.go"],
    visibility = ["//visibility:public"],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "version_test.go",
    ],
    library = ":go_default_library",
)

go_test(
    name = "go_default_test",
    srcs = ["version_test.go"],
    library = ":go_default_library",
)
