package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test", "go_binary")

go_library(
    name = "go_default_library",
    srcs = [
        "generator.go",
    ],
    data = ["//tools/codegen/pkg/interfacegen:template/ProcInterface.tmpl"],
    deps = [
        "//tools/codegen/pkg/modelgen:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

# Disabled since I need to work on making it work via bazel.
# go_test(
#     name = "small_tests",
#     size = "small",
#     srcs = [
#         "generator_test.go",
#     ],
#     library = ":go_default_library",
# )
