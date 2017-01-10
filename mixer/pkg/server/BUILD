package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "adapterManager.go",
        "configManager.go",
        "dispatchKey.go",
    ],
    deps = [
        "//adapter/denyChecker:go_default_library",
        "//adapter/genericListChecker:go_default_library",
        "//adapter/ipListChecker:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config/listChecker:go_default_library",
    ],
)
