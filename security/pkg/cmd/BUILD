load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["flag.go"],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_spf13_cobra//:go_default_library",
        "@com_github_spf13_pflag//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [ "flag_test.go" ],
    library = ":go_default_library",
)