load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "evaluator.go",
        "expr.go",
        "func.go",
        "identity.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/attribute:go_default_library",
        "//pkg/pool:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "eval_test.go",
        "expr_test.go",
        "func_test.go",
        "identity_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
