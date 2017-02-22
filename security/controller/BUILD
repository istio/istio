load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "securenaming.go",
        "storage.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/labels:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apimachinery//pkg/util/sets:go_default_library",
        "@io_k8s_apimachinery//pkg/watch:go_default_library",
        "@io_k8s_client_go//kubernetes/typed/core/v1:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_client_go//util/workqueue:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "securenaming_test.go",
        "storage_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/util/sets:go_default_library",
        "@io_k8s_client_go//kubernetes/typed/core/v1/fake:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
    ],
)
