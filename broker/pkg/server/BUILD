package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["broker.go"],
    deps = [
        "//broker/pkg/controller:go_default_library",
        "//broker/pkg/model/config:go_default_library",
        "//broker/pkg/platform/kube/crd:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_gorilla_mux//:go_default_library",
    ],
)
