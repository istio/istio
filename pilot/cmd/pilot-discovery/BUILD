load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")
load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//adapter/config/aggregate:go_default_library",
        "//adapter/config/crd:go_default_library",
        "//adapter/config/ingress:go_default_library",
        "//adapter/serviceregistry/aggregate:go_default_library",
        "//cmd:go_default_library",
        "//model:go_default_library",
        "//platform:go_default_library",
        "//platform/consul:go_default_library",
        "//platform/eureka:go_default_library",
        "//platform/kube:go_default_library",
        "//proxy:go_default_library",
        "//proxy/envoy:go_default_library",
        "//tools/version:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_istio_api//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
    ],
)

go_binary(
    name = "pilot-discovery",
    library = ":go_default_library",
    linkstamp = "istio.io/pilot/tools/version",
    visibility = ["//visibility:public"],
)
