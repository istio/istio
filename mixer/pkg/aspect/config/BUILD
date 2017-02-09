load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
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
