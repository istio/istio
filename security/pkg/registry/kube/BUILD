load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "service.go",
        "serviceaccount.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/platform/kube:go_default_library",
        "//security/pkg/pki/ca:go_default_library",
        "//security/pkg/registry:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apimachinery//pkg/watch:go_default_library",
        "@io_k8s_client_go//kubernetes/typed/core/v1:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "service_test.go",
        "serviceaccount_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//kubernetes/fake:go_default_library",
    ],
)
