load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "builder.go",
        "debug.go",
        "sort.go",
        "string.go",
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
        "//pkg/log:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)


go_test(
    name = "go_default_test",
    srcs = [
        "builder_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/runtime2/testing/data:go_default_library",
        "//mixer/pkg/runtime2/testing/util:go_default_library",
    ],
)
