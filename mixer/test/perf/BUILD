load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["minimal_test.go"],
    deps = [
        "//mixer/adapter:go_default_library",
        "//mixer/pkg/perf:go_default_library",
        "//mixer/template:go_default_library",
    ],
)
