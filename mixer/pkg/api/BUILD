package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["grpcServer.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapterManager:go_default_library",
        "//mixer/pkg/aspect:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "//mixer/pkg/runtime:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_opentracing_opentracing_go//log:go_default_library",
        "@io_istio_api//:mixer/v1",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//codes:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "grpcServer_test.go",
        "perf_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapterManager:go_default_library",
        "//mixer/pkg/aspect:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@io_istio_api//:mixer/v1",
        "@org_golang_google_grpc//:go_default_library",
    ],
)
