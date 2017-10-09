load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["compiler.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/expr:go_default_library",
        "//pkg/il:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["compiler_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/il/interpreter:go_default_library",
        "//pkg/il/testing:go_default_library",
        "//pkg/il/text:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)
