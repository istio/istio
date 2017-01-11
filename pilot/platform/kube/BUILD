load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_prefix", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "client.go",
        "config.go",
        "controller.go",
        "queue.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//pkg/api:go_default_library",
        "@io_k8s_client_go//pkg/api/errors:go_default_library",
        "@io_k8s_client_go//pkg/api/meta:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//pkg/apis/extensions/v1beta1:go_default_library",
        "@io_k8s_client_go//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//pkg/runtime:go_default_library",
        "@io_k8s_client_go//pkg/runtime/schema:go_default_library",
        "@io_k8s_client_go//pkg/runtime/serializer:go_default_library",
        "@io_k8s_client_go//pkg/util/flowcontrol:go_default_library",
        "@io_k8s_client_go//pkg/watch:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "large",
    srcs = [
        "minikube_test.go",
        "queue_test.go",
    ],
    data = ["config"],
    library = ":go_default_library",
    deps = [
        "//model:go_default_library",
        "//test:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
    ],
)

go_prefix("istio.io/manager")
