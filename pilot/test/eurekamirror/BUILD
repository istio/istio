load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["eurekamirror.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//pilot/model:go_default_library",
        "//pilot/platform/kube:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/runtime:go_default_library",
        "@io_k8s_apimachinery//pkg/watch:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
    ],
)

go_binary(
    name = "eurekamirror",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
