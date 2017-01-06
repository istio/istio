package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/mixer/pkg/registry")

DEPS = [
    "//pkg/aspect/listChecker:go_default_library",
    "//pkg/aspect/denyChecker:go_default_library",
]

go_library(
    name = "go_default_library",
    srcs = glob(
        include = ["*.go"],
        exclude = ["test_*.go"],
    ),
    deps = DEPS,
)
