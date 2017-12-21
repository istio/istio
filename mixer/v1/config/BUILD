load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["fixed_cfg.pb.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/v1/config/descriptor:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = ["cfg.proto"],
    visibility = ["//visibility:public"],
)
