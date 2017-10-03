package(default_visibility = ["//pkg/server:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "client.go",
        "conversion.go",
        "template.go",
        "types.go",
    ],
    deps = [
        "//pkg/model/config:go_default_library",
        "//pkg/testing/mock:go_default_library",
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
        "@io_k8s_client_go//plugin/pkg/client/auth/gcp:go_default_library",
        "@io_k8s_client_go//plugin/pkg/client/auth/oidc:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)

genrule(
    name = "crd",
    srcs = ["template.go"],
    outs = ["types.go"],
    cmd = "pkg/platform/kube/crd/generate.sh > $@",
    tools = ["generate.sh"],
)

go_test(
    name = "go_default_test",
    srcs = [
        "client_test.go",
        "conversion_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/model/config:go_default_library",
        "//pkg/testing/mock:go_default_library",
        "//pkg/testing/util:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)
