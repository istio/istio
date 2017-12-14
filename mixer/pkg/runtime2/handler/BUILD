load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "cleanup.go",
        "sha.go",
        "table.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/runtime2/config:go_default_library",
        "//mixer/pkg/log:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
    ],
)

