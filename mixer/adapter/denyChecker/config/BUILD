load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "google/rpc/status.proto": "github.com/googleapis/googleapis/google/rpc",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "external/com_github_googleapis_googleapis/",
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
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
