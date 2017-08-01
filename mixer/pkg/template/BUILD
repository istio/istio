package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "template.gen.go",
        "template.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/adapter/config:go_default_library",
        "//template/sample/check:go_default_library",
        "//template/sample/quota:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "template.gen_test.go",
        "template_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter/config:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
