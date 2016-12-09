package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//proto:go_proto_library.bzl", "go_proto_library")

go_proto_library(
    name = "lib_proto",
    srcs = [ 
        "label_descriptor.proto",
        "log_entry_descriptor.proto",
        "metric_descriptor.proto",
        "monitored_resource_descriptor.proto",
        "principal_descriptor.proto",
        "quota_descriptor.proto",
    ],
)
