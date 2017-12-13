load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "conversion.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_cluster_registry//pkg/apis/clusterregistry/v1alpha1:go_default_library",
        "@io_k8s_apimachinery//pkg/util/yaml:go_default_library",
    ],
)
