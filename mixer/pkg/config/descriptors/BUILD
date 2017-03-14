package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["descriptors.go"],
    deps = [
        "//pkg/config/proto:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "descriptors_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/aspect/config:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)
