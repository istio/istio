load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    protos = [
        "accessLogger.proto",
        "denyChecker.proto",
        "listChecker.proto",
        "logger.proto",
        "quota.proto",
    ],
    verbose = 0,
    visibility = [
        "//pkg/aspect:__pkg__",
        "//pkg/config:__pkg__",
    ],
)
