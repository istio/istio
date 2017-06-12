load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["config.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["config_test.go"],
    library = ":go_default_library",
    deps = ["//test/mock:go_default_library"],
)
