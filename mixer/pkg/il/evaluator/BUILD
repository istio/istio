package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["evaluator.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/attribute:go_default_library",
        "//pkg/config:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/il/compiler:go_default_library",
        "//pkg/il/interpreter:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_golang_lru//:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["evaluator_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/attribute:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/il/testing:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)
