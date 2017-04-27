load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["proxy.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//apiserver:go_default_library",
        "//model:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["proxy_test.go"],
    library = ":go_default_library",
    deps = [
        "//apiserver:go_default_library",
        "//model:go_default_library",
    ],
)
