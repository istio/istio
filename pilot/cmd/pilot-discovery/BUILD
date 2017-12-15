load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")
load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//pilot/cmd:go_default_library",
        "//pilot/cmd/pilot-discovery/server:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
    ],
)

go_binary(
    name = "pilot-discovery",
    library = ":go_default_library",
    linkstamp = "istio.io/istio/pilot/tools/version",
    visibility = ["//visibility:public"],
)
