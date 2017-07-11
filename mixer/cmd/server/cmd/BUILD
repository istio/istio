package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "inventory.go",
        "root.go",
        "server.go",
    ],
    visibility = ["//cmd:__subpackages__"],
    deps = [
        "//adapter:go_default_library",
        "//cmd/shared:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/adapterManager:go_default_library",
        "//pkg/api:go_default_library",
        "//pkg/aspect:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/config/store:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/pool:go_default_library",
        "//pkg/tracing:go_default_library",
        "//pkg/version:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_grpcecosystem_middleware//:go_default_library",
        "@com_github_grpcecosystem_opentracing//go/otgrpc:go_default_library",
        "@com_github_grpcecosystem_prometheus//:go_default_library",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_opentracing_basictracer//:go_default_library",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_prometheus_client_golang//prometheus/promhttp:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//grpclog/glogger:go_default_library",
    ],
)
