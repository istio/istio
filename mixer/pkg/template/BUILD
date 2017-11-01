package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["template.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
        "@io_istio_api//:mixer/v1/template",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["template_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
