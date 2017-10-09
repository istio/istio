package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "diag.go",
        "model.go",
        "parser.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
        "@io_istio_api//:mixer/v1/template",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["model_test.go"],
    data = ["//tools/codegen/pkg/modelgen/testdata:test_descriptors"],
    library = ":go_default_library",
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
    ],
)
