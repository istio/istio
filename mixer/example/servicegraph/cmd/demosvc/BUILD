package(default_visibility = ["//visibility:private"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "@com_github_prometheus_client_golang//prometheus:go_default_library",
        "@com_github_prometheus_client_golang//prometheus/promhttp:go_default_library",
    ],
)

go_binary(
    name = "demosvc",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
