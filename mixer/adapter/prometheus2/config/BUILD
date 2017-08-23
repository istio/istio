load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
    },
    imports = [
        "external/com_github_gogo_protobuf",
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
    ],
    protos = [
        "config.proto",
    ],
    verbose = 0,
    visibility = ["//adapter/prometheus2:__pkg__"],
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
    ],
)
