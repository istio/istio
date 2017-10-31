load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "ca.go",
        "generate_cert.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//security/pkg/pki:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//kubernetes/typed/core/v1:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "ca_test.go",
        "generate_cert_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//security/pkg/pki:go_default_library",
        "//security/pkg/pki/testutil:go_default_library",
        "@io_k8s_client_go//kubernetes/fake:go_default_library",
        "@io_k8s_client_go//testing:go_default_library",
    ],
)
