package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "memQuota.go",
        "rollingWindow.go",
    ],
    deps = [
        "//adapter/memQuota/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/pool:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "memQuota_test.go",
        "rollingWindow_test.go",
    ],
    library = ":go_default_library",
    deps = ["//pkg/adapter/test:go_default_library"],
)
