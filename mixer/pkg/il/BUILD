load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "builder.go",
        "convert.go",
        "function.go",
        "opcode.go",
        "program.go",
        "strings.go",
        "types.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/attribute:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "builder_test.go",
        "function_test.go",
        "strings_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
