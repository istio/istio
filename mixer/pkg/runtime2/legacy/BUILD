load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "env.go",
        "logger.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)


go_test(
    name = "go_default_test",
    srcs = [
        "env_test.go",
    ],
    library = ":go_default_library",
)
