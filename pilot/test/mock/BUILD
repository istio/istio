load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "secret.go",
        "service.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "//model/test:go_default_library",
        "//proxy:go_default_library",
        "//test/util:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "secret_test.go",
        "service_test.go",
    ],
    library = ":go_default_library",
    deps = ["//model:go_default_library"],
)
