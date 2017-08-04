load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "controller.go",
        "conversion.go",
        "monitor.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:go_default_library",
        "@com_github_hashicorp_consul//api:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "conversion_test.go",
        "controller_test.go"
    ],
    library = ":go_default_library",
    deps = [
        "//model:go_default_library",
        "@com_github_hashicorp_consul//api:go_default_library",
    ],
)