package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "basictracing.go",
    ],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@com_github_opentracing_basictracer//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "basictracing_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_istio_api//:mixer/v1",
        "@com_github_opentracing_opentracing_go//mocktracer:go_default_library",
    ],
)
