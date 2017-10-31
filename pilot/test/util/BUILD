load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "diff.go",
        "kubernetes.go",
        "shell.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@com_github_pmezard_go_difflib//difflib:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
    ],
)
