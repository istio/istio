package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "adapterInfoRegistry.go",
        "inventory.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/config/crd:go_default_library",
        "//mixer/pkg/config/store:go_default_library",
        "//pkg/log:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["adapterInfoRegistry_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/template:go_default_library",
        "//mixer/template/sample:go_default_library",
        "//mixer/template/sample/report:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
