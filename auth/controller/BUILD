load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["securenaming.go"],
    visibility = ["//visibility:public"],
    deps = [
        "@io_k8s_client_go//kubernetes/typed/core/v1:go_default_library",
        "@io_k8s_client_go//pkg/api:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//pkg/labels:go_default_library",
        "@io_k8s_client_go//pkg/runtime:go_default_library",
        "@io_k8s_client_go//pkg/watch:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["securenaming_test.go"],
    library = ":go_default_library",
    deps = [
        "@io_k8s_client_go//kubernetes/typed/core/v1/fake:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
    ],
)
