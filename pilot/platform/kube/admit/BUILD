load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["admit.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/config/crd:go_default_library",
        "//model:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_api//admission/v1alpha1:go_default_library",
        "@io_k8s_api//admissionregistration/v1alpha1:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/api/errors:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/fields:go_default_library",
        "@io_k8s_apiserver//pkg/admission:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//kubernetes/typed/admissionregistration/v1alpha1:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["admit_test.go"],
    data = glob(["testdata/**"]),
    library = ":go_default_library",
    deps = [
        "//adapter/config/crd:go_default_library",
        "//model:go_default_library",
        "//model/test:go_default_library",
        "//platform/kube:go_default_library",
        "//platform/kube/admit/testcerts:go_default_library",
        "//test/mock:go_default_library",
        "@io_k8s_api//admission/v1alpha1:go_default_library",
        "@io_k8s_api//admissionregistration/v1alpha1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apiserver//pkg/admission:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//kubernetes/fake:go_default_library",
    ],
)
