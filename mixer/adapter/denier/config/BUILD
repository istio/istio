load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
        "google/rpc/status.proto": "github.com/googleapis/googleapis/google/rpc",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "external/com_github_gogo_protobuf",
        "external/com_github_google_protobuf/src",
        "external/com_github_googleapis_googleapis/",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_googleapis_googleapis//:status_proto",
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
    ],
    protos = [
        "config.proto",
    ],
    verbose = 0,
    visibility = ["//adapter/denier:__pkg__"],
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
