package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["ipListChecker.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/ipListChecker/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["ipListChecker_test.go"],
    library = ":go_default_library",
    deps = [
        "//adapter/ipListChecker/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
    ],
)
