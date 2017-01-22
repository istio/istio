package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "aspect.go",
        "builder.go",
    ],
    deps = [
        "//adapter/ipListChecker/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = ["adapter_test.go"],
    library = ":go_default_library",
    deps = ["//pkg/adapter/testing:go_default_library"],
)
