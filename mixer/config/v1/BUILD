package(default_visibility = ["//visibility:public"])

load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    protos = [ 
        "label_descriptor.proto",
        "log_entry_descriptor.proto",
        "metric_descriptor.proto",
        "monitored_resource_descriptor.proto",
        "principal_descriptor.proto",
        "quota_descriptor.proto",
    ],
)
