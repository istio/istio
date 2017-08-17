load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "client.go",
        "config.go",
        "controller.go",
        "conversion.go",
        "types.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_k8s_apiextensions_apiserver//pkg/apis/apiextensions/v1beta1:go_default_library",
        "@io_k8s_apiextensions_apiserver//pkg/client/clientset/clientset:go_default_library",
        "@io_k8s_apimachinery//pkg/api/errors:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime/schema:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime/serializer:go_default_library",
        "@io_k8s_apimachinery//pkg/util/wait:go_default_library",
        "@io_k8s_apimachinery//pkg/watch:go_default_library",
        "@io_k8s_client_go//plugin/pkg/client/auth/gcp:go_default_library",
        "@io_k8s_client_go//plugin/pkg/client/auth/oidc:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)

genrule(
    name = "crd",
    srcs = ["config.go"],
    outs = ["types.go"],
    cmd = "adapter/config/crd/generate.sh > $@",
    tools = ["generate.sh"],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "client_test.go",
        "controller_test.go",
        "conversion_test.go",
    ],
    data = ["//platform/kube:kubeconfig"],
    library = ":go_default_library",
    deps = [
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "//test/mock:go_default_library",
        "//test/util:go_default_library",
    ],
)
