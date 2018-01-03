load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "admit.go",
        "init.go",
        "store.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/config/store:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@io_k8s_api//admission/v1alpha1:go_default_library",
        "@io_k8s_api//admissionregistration/v1alpha1:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/api/errors:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1/unstructured:go_default_library",
        "@io_k8s_apimachinery//pkg/fields:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime/schema:go_default_library",
        "@io_k8s_apiserver//pkg/admission:go_default_library",
        "@io_k8s_client_go//discovery:go_default_library",
        "@io_k8s_client_go//dynamic:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//kubernetes/typed/admissionregistration/v1alpha1:go_default_library",
        "@io_k8s_client_go//plugin/pkg/client/auth/gcp:go_default_library",
        "@io_k8s_client_go//plugin/pkg/client/auth/oidc:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "admit_test.go",
        "store_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/config/store:go_default_library",
        "//pilot/platform/kube/admit/testcerts:go_default_library",  # for testing cert data
        "//tests/k8s:go_default_library",
        "@io_k8s_api//admission/v1alpha1:go_default_library",
        "@io_k8s_api//admissionregistration/v1alpha1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1/unstructured:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apimachinery//pkg/watch:go_default_library",
        "@io_k8s_apiserver//pkg/admission:go_default_library",
        "@io_k8s_client_go//discovery:go_default_library",
        "@io_k8s_client_go//discovery/fake:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//kubernetes/fake:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//testing:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)
