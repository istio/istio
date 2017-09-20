load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "collateral.go",
        "inject.go",
        "main.go",
        "mixer.go",
        "register.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//adapter/config/crd:go_default_library",
        "//cmd:go_default_library",
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "//platform/kube/inject:go_default_library",
        "//tools/version:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@com_github_spf13_cobra//doc:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/util/yaml:go_default_library",
        "@io_k8s_client_go//discovery:go_default_library",
        "@io_k8s_client_go//dynamic:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
        "@io_k8s_client_go//tools/clientcmd/api:go_default_library",
    ],
)

go_binary(
    name = "istioctl",
    library = ":go_default_library",
    linkstamp = "istio.io/pilot/tools/version",
    visibility = ["//visibility:public"],
)
