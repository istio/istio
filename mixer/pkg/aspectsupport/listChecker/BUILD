package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "manager.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/listChecker:go_default_library",
        "//pkg/aspectsupport:go_default_library",
        "//pkg/aspectsupport/listChecker/config:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/expr:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    ],
)

go_test(
    name = "manager_test",
    size = "small",
    srcs = ["manager_test.go"],
    library = ":go_default_library",
)
