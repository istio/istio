load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "registry.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/platform/kube:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_k8s_api//core/v1:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "registry_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
    ],
)
