load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
        "google/rpc/status.proto": "istio.io/api/google/rpc",
        "google/protobuf/timestamp.proto": "github.com/gogo/protobuf/types",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "external/com_github_gogo_protobuf",
        "external/com_google_protobuf/src",
        "external/com_github_googleapis_googleapis",
        "../../external/com_github_gogo_protobuf",
        "../../external/com_google_protobuf/src",
        "../../external/com_github_googleapis_googleapis",
    ],
    inputs = [
        "@com_google_protobuf//:well_known_protos",
        "@com_github_googleapis_googleapis//:status_proto",
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
    ],
    protos = [":protos"],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = True,
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "//google/rpc:go_default_library",
    ],
)

cc_proto_library(
    name = "cc_protos",
    deps = [":mixer_protos"],
    visibility = ["//visibility:public"],
)

proto_library(
    name = "mixer_protos",
    srcs = [":protos"],
    deps = [
        "@com_google_protobuf//:any_proto",
        "@com_google_protobuf//:duration_proto",
        "@com_google_protobuf//:timestamp_proto",
        "//gogoproto:gogoproto_protos",
        "//google/rpc:rpc_protos",
    ],
    visibility = ["//visibility:public"],
)

filegroup(
    name = "protos",
    srcs = [
        "attributes.proto",
        "check.proto",
        "report.proto",
        "service.proto",
    ],
    visibility = ["//visibility:public"],
)

filegroup(
    name = "attributes_file",
    srcs = [":global_dictionary.yaml"],
    visibility = ["//visibility:public"],
)
