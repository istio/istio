load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    protos = [
        "accessLogs.proto",
        "applicationLogs.proto",
        "denials.proto",
        "lists.proto",
        "quotas.proto",
    ],
    verbose = 0,
    visibility = [
        "//pkg/aspect:__pkg__",
        "//pkg/config:__pkg__",
    ],
)
