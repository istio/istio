load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "main.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "//proxy/envoy:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_k8s_client_go//pkg/api:go_default_library",
    ],
)

go_binary(
    name = "manager",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
