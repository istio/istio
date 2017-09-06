load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
        "google/api/metric.proto": "google.golang.org/genproto/googleapis/api/metric",
    },
    imports = [
        "external/com_github_gogo_protobuf",
        "external/com_github_google_protobuf/src",
        "external/com_github_googleapis_googleapis",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
        "@com_github_googleapis_googleapis//:label_proto",
        "@com_github_googleapis_googleapis//:metric_proto",
    ],
    protos = [
        "config.proto",
    ],
    verbose = 0,
    visibility = ["//adapter/stackdriver:__subpackages__"],
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@org_golang_google_genproto//googleapis/api/metric:go_default_library",
    ],
)
