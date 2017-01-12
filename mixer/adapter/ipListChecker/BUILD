package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "adapter.go",
        "aspect.go",
    ],
    deps = [
        "//adapter/ipListChecker/config:go_default_library",
        "//pkg/aspect:go_default_library",
        "//pkg/aspect/listChecker:go_default_library",
        "//pkg/registry:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
    ],
)

go_test(
    name = "ip_list_checker_test",
    size = "small",
    srcs = ["adapter_test.go"],
    library = ":go_default_library",
    deps = ["//pkg/adaptertesting:go_default_library"],
)
