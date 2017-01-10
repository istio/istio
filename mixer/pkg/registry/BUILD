package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/mixer/pkg/registry")

go_library(
    name = "go_default_library",
    srcs = glob(
        include = ["*.go"],
        exclude = ["test_*.go"],
    ),
    deps = [
        "//pkg/aspect/denyChecker:go_default_library",
        "//pkg/aspect/listChecker:go_default_library",
        "//pkg/aspect/logger:go_default_library",
        "//pkg/aspect/quota:go_default_library",
    ],
)
