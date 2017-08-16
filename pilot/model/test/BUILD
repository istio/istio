load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["mock_config.pb.go"],
    visibility = ["//visibility:public"],
    deps = ["@com_github_golang_protobuf//proto:go_default_library"],
)

filegroup(
    name = "go_default_library_protos",
    srcs = ["mock_config.proto"],
    visibility = ["//visibility:public"],
)
