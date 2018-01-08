load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "extensions.pb.go",
        "standard_types.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "extensions.proto",
        "standard_types.proto",
    ],
    visibility = ["//visibility:public"],
)
