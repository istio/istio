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


go_test(
    name = "go_default_test",
    srcs = [
        "cleanup_test.go",
        "common_test.go",
        "factory_test.go",
        "instantiate_test.go",
        "table_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/runtime2/config/testutil:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",

        "//mixer/pkg/log:go_default_library",
        "@org_uber_go_zap//zapcore:go_default_library",
    ],
)
