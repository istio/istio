load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "service_class.pb.go",
        "service_plan.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = ["@com_github_golang_protobuf//proto:go_default_library"],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "service_class.proto",
        "service_plan.proto",
    ],
    visibility = ["//visibility:public"],
)
