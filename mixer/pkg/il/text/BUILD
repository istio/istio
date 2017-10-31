load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "read.go",
        "scanner.go",
        "write.go",
    ],
    visibility = ["//visibility:public"],
    deps = ["//mixer/pkg/il:go_default_library"],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "read_test.go",
        "scanner_test.go",
        "write_test.go",
    ],
    library = ":go_default_library",
    deps = ["//mixer/pkg/il:go_default_library"],
)
