load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//pilot/cmd:go_default_library",
        "//pilot/platform/kube:go_default_library",
        "//pilot/platform/kube/inject:go_default_library",
        "//pilot/tools/version:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
    ],
)

go_binary(
    name = "sidecar-initializer",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
