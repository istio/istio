package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "crd.go",
        "inventory.go",
        "root.go",
        "server.go",
        "validator.go",
    ],
    visibility = [
        "//mixer/cmd:__subpackages__",
        "//mixer/pkg/mock:__subpackages__",
    ],
    deps = [
        "//mixer/cmd/shared:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapterManager:go_default_library",
        "//mixer/pkg/api:go_default_library",
        "//mixer/pkg/aspect:go_default_library",
        "//mixer/pkg/config:go_default_library",
        "//mixer/pkg/config/crd:go_default_library",
        "//mixer/pkg/config/store:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il/evaluator:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "//mixer/pkg/runtime:go_default_library",
        "//mixer/pkg/server:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/pkg/tracing:go_default_library",
        "//mixer/pkg/version:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_grpc_ecosystem_go_grpc_middleware//:go_default_library",
        "@com_github_grpc_ecosystem_go_grpc_prometheus//:go_default_library",
        "@com_github_grpc_ecosystem_grpc_opentracing//go/otgrpc:go_default_library",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_prometheus_client_golang//prometheus/promhttp:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//grpclog/glogger:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["crd_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/template:go_default_library",
    ],
)
