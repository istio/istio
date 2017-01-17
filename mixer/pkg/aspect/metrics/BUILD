package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "metrics.go",
    ],
    deps = [
        "//pkg/aspect:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = ["metrics_test.go"],
    library = ":go_default_library",
)
