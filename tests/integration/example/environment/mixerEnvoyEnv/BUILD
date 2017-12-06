load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "mixer_envoy_env.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//tests/integration/framework:go_default_library",
        "//tests/integration/component/fortio_server:go_default_library",
        "//tests/integration/component/mixer:go_default_library",
        "//tests/integration/component/proxy:go_default_library",
    ],
)
