load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "controller.go",
        "conversion.go",
        "status.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/model:go_default_library",
        "//pilot/platform/kube:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:proxy/v1/config",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_api//extensions/v1beta1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apimachinery//pkg/util/intstr:go_default_library",
        "@io_k8s_apimachinery//pkg/watch:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_ingress//core/pkg/ingress/status:go_default_library",
        "@io_k8s_ingress//core/pkg/ingress/store:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "controller_test.go",
        "conversion_test.go",
        "status_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pilot/model:go_default_library",
        "//pilot/platform/kube:go_default_library",
        "//pilot/proxy:go_default_library",
        "//pilot/test/mock:go_default_library",
        "//pilot/test/util:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_istio_api//:proxy/v1/config",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_api//extensions/v1beta1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/util/intstr:go_default_library",
        "@io_k8s_client_go//kubernetes/fake:go_default_library",
        "@io_k8s_ingress//core/pkg/ingress/annotations/class:go_default_library",
    ],
)
