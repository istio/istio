package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["redisstore.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/config/store:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_mediocregopher_radix_v2//pubsub:go_default_library",
        "@com_github_mediocregopher_radix_v2//redis:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["redisstore_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/config/store:go_default_library",
        "@com_github_alicebob_miniredis//:go_default_library",
    ],
)
