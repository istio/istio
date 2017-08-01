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
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "builder_test.go",
        "convert_test.go",
        "function_test.go",
        "opcode_test.go",
        "program_test.go",
        "strings_test.go",
        "types_test.go",
    ],
    library = ":go_default_library",
)
