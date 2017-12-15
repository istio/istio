package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "expression.go",
        "expressionbuilder.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il/compiler:go_default_library",
        "//mixer/pkg/il/interpreter:go_default_library",
        "//mixer/pkg/il/runtime:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "medium",
    srcs = [
        "expressionBenchmark_test.go",
        "expressionbuilder_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/config/descriptor:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/il/testing:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:descriptor",
    ],
)
