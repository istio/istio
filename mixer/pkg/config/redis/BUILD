package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "redisstore.go",
    ],
    deps = [
        "//pkg/config/store:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_redis_client//pubsub:go_default_library",
        "@com_github_redis_client//redis:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "redisstore_test.go",
    ],
    library = ":go_default_library",
    deps = [
        ":go_default_library",
        "@com_github_mini_redis//:go_default_library",
    ],
)
