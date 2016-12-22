package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

DEPS = [
    "//pkg/adapter:go_default_library",
    "//pkg/attribute:go_default_library",
    "//pkg/server:go_default_library",
    "@com_github_golang_glog//:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_golang_protobuf//ptypes:go_default_library",
    "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
    "@org_golang_google_grpc//:go_default_library",
    "@org_golang_google_grpc//credentials:go_default_library",
    "@com_github_istio_api//:go_default_library",
]

go_library(
    name = "go_default_library",
    srcs = [
        "grpcServer.go",
        "methodHandlers.go",
    ],
    deps = DEPS,
)

go_test(
    name = "grpc_server_test",
    size = "small",
    srcs = ["grpcServer_test.go"],
    library = ":go_default_library",
)
