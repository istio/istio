load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "flags.go",
        "istio_ca_cert_rotation_env.go",
        "istio_ca_node_agent_env.go",
        "istio_ca_secret_env.go",
        "kubernetes_pod.go",
        "kubernetes_service.go",
        "kubernetes_utils.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/platform/kube:go_default_library",
        "//security/pkg/pki/ca/controller:go_default_library",
        "//security/pkg/pki/testutil:go_default_library",
        "//tests/integration/framework:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_api//rbac/v1beta1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/labels:go_default_library",
        "@io_k8s_apimachinery//pkg/util/uuid:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//plugin/pkg/client/auth:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)
