load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["compiler.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il:go_default_library",
        "//pkg/log:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["compiler_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/config/descriptor:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il/interpreter:go_default_library",
        "//mixer/pkg/il/runtime:go_default_library",
        "//mixer/pkg/il/testing:go_default_library",
        "//mixer/pkg/il/text:go_default_library",
    ],
)
