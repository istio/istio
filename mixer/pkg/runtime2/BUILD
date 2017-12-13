load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "controller.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        # Only for the dispatcher interface.
        "//mixer/pkg/runtime:go_default_library",

        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/il/compiled:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/pkg/runtime2/config:go_default_library",
        "//mixer/pkg/runtime2/dispatcher:go_default_library",
        "//mixer/pkg/runtime2/handler:go_default_library",
        "//mixer/pkg/runtime2/routing:go_default_library",
    ],
)

