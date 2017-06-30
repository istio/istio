package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "diag.go",
        "model.go",
        "parser.go",
    ],
    deps = [
        "//tools/codegen/pkg/template_extension:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "model_test.go",
    ],
    data = ["//tools/codegen/pkg/modelgen/testdata:test_descriptors"],
    library = ":go_default_library",
)
