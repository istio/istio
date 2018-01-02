package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["tracing.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/log:go_default_library",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_uber_jaeger_client_go//:go_default_library",
        "@com_github_uber_jaeger_client_go//log:go_default_library",
        "@com_github_uber_jaeger_client_go//transport:go_default_library",
        "@com_github_uber_jaeger_client_go//transport/zipkin:go_default_library",
        "@org_uber_go_zap//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["tracing_test.go"],
    library = ":go_default_library",
)
