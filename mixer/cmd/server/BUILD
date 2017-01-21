package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "adapter.go",
        "main.go",
        "server.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//adapter/denyChecker:go_default_library",
        "//pkg/api:go_default_library",
        "//pkg/aspect:go_default_library",
        "//pkg/aspect/denyChecker:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/tracing:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//ptypes/struct:go_default_library",
        "@com_github_istio_api//:mixer/v1/config",
        "@com_github_opentracing_basictracer//:go_default_library",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_binary(
    name = "mixs",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
