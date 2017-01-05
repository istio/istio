load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "agent.go",
        "config.go",
        "discovery.go",
        "resources.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
    ],
)
