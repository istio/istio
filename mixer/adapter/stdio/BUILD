package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["stdio.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/stdio/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//template/logentry:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
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
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
