load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "rbac.proto",
    ],
    verbose = 0,
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
    ],
    visibility = ["//visibility:public"],
)
