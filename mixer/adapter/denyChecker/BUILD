package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["denyChecker.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/denyChecker/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["denyChecker_test.go"],
    library = ":go_default_library",
    deps = [
        "//adapter/denyChecker/config:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
