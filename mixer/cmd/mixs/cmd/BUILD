package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "crd.go",
        "root.go",
        "server.go",
        "validator.go",
    ],
    visibility = [
        "//mixer/cmd:__subpackages__",
        "//mixer/pkg/mock:__subpackages__",
    ],
    deps = [
        "//mixer/cmd/shared:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/config:go_default_library",
        "//mixer/pkg/config/crd:go_default_library",
        "//mixer/pkg/config/store:go_default_library",
        "//mixer/pkg/il/evaluator:go_default_library",
        "//mixer/pkg/runtime:go_default_library",
        "//mixer/pkg/server:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/pkg/version:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["crd_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/template:go_default_library",
    ],
)
