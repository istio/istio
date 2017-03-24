package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "root.go",
    ],
    visibility = ["//cmd:__subpackages__"],
    deps = [
        "//cmd/client/cmd:go_default_library",
        "//cmd/server/cmd:go_default_library",
        "//cmd/shared:go_default_library",
        "//pkg/tracing:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_opentracing_basictracer//:go_default_library",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_opentracing_opentracing_go//ext:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@com_github_spf13_cobra//doc:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)
