load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "cleanup.go",
        "factory.go",
        "instantiate.go",
        "signature.go",
        "table.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il/evaluator:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/pkg/runtime2/config:go_default_library",
        "//mixer/pkg/log:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:descriptor",
    ],
)

