load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "evaluator.go",
        "identity.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/attribute:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "identity_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_istio_api//:mixer/v1",
    ],
)
