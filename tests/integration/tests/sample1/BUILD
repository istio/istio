load("@io_bazel_rules_go//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    srcs = ["sample1_test.go"],
    tags = ["manual"],
    deps = [
        "//tests/integration/framework/environment:go_default_library",
        "//tests/integration/framework:go_default_library",
        "//tests/util:go_default_library",
    ],
)
