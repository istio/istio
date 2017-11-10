load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "fakebag.go",
        "tests.go",
        "util.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)
