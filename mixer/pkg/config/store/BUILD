package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "convert.go",
        "fsstore2.go",
        "memstore.go",
        "queue.go",
        "store2.go",
        "validator.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/log:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "convert_test.go",
        "fsstore2_test.go",
        "queue_test.go",
        "store2_test.go",
        "validator_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/config/proto:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
    ],
)
