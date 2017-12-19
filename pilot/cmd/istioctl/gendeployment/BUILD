load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "cmd.go",
        "helm.go",
        "yaml.go",
    ],
    importpath = "istio.io/istio/pilot/cmd/istioctl/gendeployment",
    visibility = ["//pilot/cmd/istioctl:__subpackages__"],
    deps = [
        "@com_github_spf13_cobra//:go_default_library",
        "@io_k8s_helm//pkg/chartutil:go_default_library",
        "@io_k8s_helm//pkg/engine:go_default_library",
        "@io_k8s_helm//pkg/proto/hapi/chart:go_default_library",
        "@io_k8s_helm//pkg/timeconv:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "cmd_test.go",
        "helm_test.go",
    ],
    data = glob(["testdata/*.golden"]),
    embed = [":go_default_library"],
    importpath = "istio.io/istio/pilot/cmd/istioctl/gendeployment",
)
