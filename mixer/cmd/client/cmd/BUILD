package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "check.go",
        "report.go",
        "root.go",
        "util.go",
    ],
    visibility = ["//cmd:__subpackages__"],
    deps = [
        "//cmd/shared:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/tracing/zipkin:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_grpc_ecosystem_grpc_opentracing//go/otgrpc:go_default_library",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_opentracing_opentracing_go//ext:go_default_library",
        "@com_github_openzipkin_zipkin_go_opentracing//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_istio_api//:mixer/v1",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["util_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/attribute:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//codes:go_default_library",
    ],
)
