package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "checker.go",
        "evaluator.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/config:go_default_library",
        "//mixer/pkg/config/descriptor:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il:go_default_library",
        "//mixer/pkg/il/compiler:go_default_library",
        "//mixer/pkg/il/interpreter:go_default_library",
        "//mixer/pkg/il/runtime:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_hashicorp_golang_lru//:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:descriptor",
    ],
)

go_test(
    name = "go_default_test",
    size = "medium",
    srcs = [
        "checker_test.go",
        "evaluatorBenchmark_test.go",
        "evaluator_test.go",
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
