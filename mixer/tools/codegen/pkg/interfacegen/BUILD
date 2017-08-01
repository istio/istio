package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["generator.go"],
    visibility = ["//tools:__subpackages__"],
    deps = [
        "//tools/codegen/pkg/interfacegen/template:go_default_library",
        "//tools/codegen/pkg/modelgen:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
        "@org_golang_x_tools//imports:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["generator_test.go"],
    data = [
        "//tools/codegen/pkg/interfacegen/testdata:descriptors",
        "//tools/codegen/pkg/interfacegen/testdata:error_test_files",
        "//tools/codegen/pkg/interfacegen/testdata:generated_files",
        "//tools/codegen/pkg/interfacegen/testdata:golden_files",
    ],
    library = ":go_default_library",
)
