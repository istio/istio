package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "cache.go",
        "lruCache.go",
        "ttlCache.go",
    ],
    visibility = ["//visibility:public"],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "cache_test.go",
        "lruCache_test.go",
        "ttlCache_test.go",
    ],
    library = ":go_default_library",
)
