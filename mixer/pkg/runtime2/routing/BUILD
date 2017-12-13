load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "builder.go",
        "destination.go",
        "table.go",
        "util.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/il/compiled:go_default_library",
        "//mixer/pkg/runtime2/config:go_default_library",
        "//mixer/pkg/runtime2/handler:go_default_library",
        "//mixer/pkg/log:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

