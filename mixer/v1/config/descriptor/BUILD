load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "log_entry_descriptor.pb.go",
        "metric_descriptor.pb.go",
        "monitored_resource_descriptor.pb.go",
        "principal_descriptor.pb.go",
        "quota_descriptor.pb.go",
        "value_type.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "log_entry_descriptor.proto",
        "metric_descriptor.proto",
        "monitored_resource_descriptor.proto",
        "principal_descriptor.proto",
        "quota_descriptor.proto",
        "value_type.proto",
    ],
    visibility = ["//visibility:public"],
)
