package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["broker.go"],
    deps = [
        "//pkg/controller:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_gorilla_mux//:go_default_library",
    ],
)
