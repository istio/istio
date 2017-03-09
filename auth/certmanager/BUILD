load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "ca.go",
        "generate_cert.go",
    ],
    visibility = ["//visibility:public"],
)

go_test(
    name = "go_default_test",
    srcs = ["generate_cert_test.go"],
    library = ":go_default_library",
)
