load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    protos = glob(["*.proto"]),
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
)
