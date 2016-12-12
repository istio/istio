package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "bindingConfig.go",
        "bindingEvaluator.go",
        "configBlock.go",
    ],
    deps = [
        "//adapters:go_default_library",
    ]
)
