load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

DESCRIPTOR_FILE_GROUP = [
    "log_entry_descriptor.proto",
    "metric_descriptor.proto",
    "monitored_resource_descriptor.proto",
    "principal_descriptor.proto",
    "quota_descriptor.proto",
    "value_type.proto",
]

gogoslick_proto_library(
    name = "descriptor",
    importmap = {
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = DESCRIPTOR_FILE_GROUP,
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
    deps = [
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)

filegroup(
    name = "protos",
    srcs = DESCRIPTOR_FILE_GROUP,
    visibility = ["//visibility:public"],
)
