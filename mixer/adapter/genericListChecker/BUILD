package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "aspect.go",
        "builder.go",
    ],
    deps = ["//pkg/adapter:go_default_library"],
)

go_test(
    name = "generic_list_checker_test",
    size = "small",
    srcs = ["builder_test.go"],
    library = ":go_default_library",
    deps = ["//pkg/adaptertesting:go_default_library"],
)
