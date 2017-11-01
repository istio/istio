package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["stdio.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/stdio/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@org_uber_go_zap//:go_default_library",
        "@org_uber_go_zap//zapcore:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["stdio_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter/stdio/config:go_default_library",
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
        "@org_uber_go_zap//:go_default_library",
        "@org_uber_go_zap//zapcore:go_default_library",
    ],
)
