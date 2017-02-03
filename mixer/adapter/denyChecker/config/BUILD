load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogo_proto_library")

gogo_proto_library(
    name = "go_default_library",
    importmap = {
        "google/rpc/status.proto": "google.golang.org/genproto/googleapis/rpc/status",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "external/com_github_googleapis_googleapis",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_googleapis_googleapis//:status_proto",
    ],
    protos = [
        "config.proto",
    ],
    verbose = 0,
    visibility = ["//adapter/denyChecker:__pkg__"],
    deps = [
        "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
        "@com_github_googleapis_googleapis//:go_status_proto",
    ],
)
