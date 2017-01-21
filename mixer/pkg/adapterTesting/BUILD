package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    testonly = 1,
    srcs = [
        "adapters.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/denyChecker:go_default_library",
        "//pkg/adapter/listChecker:go_default_library",
        "//pkg/adapter/logger:go_default_library",
        "//pkg/adapter/quota:go_default_library",
        "//pkg/registry:go_default_library",
    ],
)
