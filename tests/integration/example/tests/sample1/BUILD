load("@io_bazel_rules_go//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    srcs = ["sample1_test.go"],
    tags = ["manual"],
    deps = [
        "//tests/integration/component/fortio_server:go_default_library",
        "//tests/integration/component/mixer:go_default_library",
        "//tests/integration/component/proxy:go_default_library",
        "//tests/integration/example/environment/appOnlyEnv:go_default_library",
        "//tests/integration/example/environment/mixerEnvoyEnv:go_default_library",
        "//tests/integration/framework:go_default_library",
    ],
)
