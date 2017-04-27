load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "inject.go",
        "main.go",
        "mixer.go",
        "collateral.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//apiserver:go_default_library",
        "//client/proxy:go_default_library",
        "//cmd:go_default_library",
        "//cmd/version:go_default_library",
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "//platform/kube/inject:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@com_github_spf13_cobra//doc:go_default_library",
        "@io_k8s_apimachinery//pkg/util/yaml:go_default_library",
        "@io_k8s_client_go//pkg/api:go_default_library",
    ],
)

go_binary(
    name = "istioctl",
    library = ":go_default_library",
    linkstamp = "istio.io/manager/cmd/version",
    visibility = ["//visibility:public"],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "istioctl_test.go",
        "mixer_test.go",
    ],
    data = glob(["testdata/*.yaml"]) + [
        "//platform/kube:kubeconfig",
    ],
    library = ":go_default_library",
    deps = [
        "//apiserver:go_default_library",
        "//model:go_default_library",
        "//test/util:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_googleapis_googleapis//:go_default_library",
    ],
)
