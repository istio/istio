load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "controller.go",
        "monitor.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = [
        "config_test.go",
        "controller_test.go",
        "monitor_test.go",
    ],
    deps = [
        ":go_default_library",
        "//model:go_default_library",
        "//test/mock:go_default_library",
    ],
)
