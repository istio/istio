package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "driver.go",
        "redisquota.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/memQuota/util:go_default_library",
        "//adapter/redisquota/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "@com_github_mediocregopher_radix_v2//pool:go_default_library",
        "@com_github_mediocregopher_radix_v2//redis:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "driver_test.go",
        "redisquota_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//adapter/redisquota/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "@com_github_alicebob_miniredis//:go_default_library",
        "@com_github_bsm_redeo//:go_default_library",
    ],
)
