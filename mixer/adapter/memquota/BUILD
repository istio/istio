package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "dedup.go",
        "keys.go",
        "memquota.go",
        "rollingWindow.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/memquota/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "//mixer/template/quota:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "memquota_test.go",
        "rollingWindow_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter/memquota/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/template/quota:go_default_library",
    ],
)
