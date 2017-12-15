load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "cmd.go",
        "helm.go",
        "yaml.go",
    ],
    importpath = "istio.io/istio/pilot/cmd/istioctl/controlplane",
    visibility = ["//pilot/cmd/istioctl:__subpackages__"],
    deps = [
        "@com_github_spf13_cobra//:go_default_library",
        "@io_k8s_helm//pkg/chartutil:go_default_library",
        "@io_k8s_helm//pkg/engine:go_default_library",
        "@io_k8s_helm//pkg/proto/hapi/chart:go_default_library",
        "@io_k8s_helm//pkg/timeconv:go_default_library",
    ],
)
