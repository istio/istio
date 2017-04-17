package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "inventory.go",
    ],
    deps = [
        "//adapter/denyChecker:go_default_library",
        "//adapter/genericListChecker:go_default_library",
        "//adapter/ipListChecker:go_default_library",
        "//adapter/kubernetes:go_default_library",
        "//adapter/memQuota:go_default_library",
        "//adapter/prometheus:go_default_library",
        "//adapter/redisquota:go_default_library",
        "//adapter/statsd:go_default_library",
        "//adapter/stdioLogger:go_default_library",
        "//pkg/adapter:go_default_library",
    ],
)
