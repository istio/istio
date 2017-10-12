load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "external/com_google_protobuf/src",
        "../../external/com_google_protobuf/src",
    ],
    inputs = [
        "@com_google_protobuf//:well_known_protos",
    ],
    protos = [
        ":protos",
    ],
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
