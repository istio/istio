load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["inject.go"],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/util/intstr:go_default_library",
        "@io_k8s_apimachinery//pkg/util/yaml:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//pkg/apis/batch/v1:go_default_library",
        "@io_k8s_client_go//pkg/apis/extensions/v1beta1:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["inject_test.go"],
    data = glob(["testdata/*.yaml*"]),
    library = ":go_default_library",
    deps = [
        "//proxy:go_default_library",
        "//test/util:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)
