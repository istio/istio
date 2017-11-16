load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")
load("@com_google_protobuf//:protobuf.bzl", "cc_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
        "mixer/v1/attributes.proto": "istio.io/api/mixer/v1",
        "mixer/v1/config/client/api_spec.proto": "istio.io/api/mixer/v1/config/client",
        "mixer/v1/config/client/quota.proto": "istio.io/api/mixer/v1/config/client",
        "mixer/v1/config/client/service.proto": "istio.io/api/mixer/v1/config/client",
        "mixer/v1/config/client/client_config.proto": "istio.io/api/mixer/v1/config/client",
        "mixer/v1/config/client/auth.proto": "istio.io/api/mixer/v1/config/client",
    },
    imports = [
        "external/com_github_gogo_protobuf",
        "external/com_github_google_protobuf/src",

    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
        "//mixer/v1:attributes",
    ],
    protos = [
        "service.proto",
        "api_spec.proto",
        "quota.proto",
        "client_config.proto",
        "auth.proto",
    ],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "//mixer/v1:go_default_library",
    ],
)

cc_proto_library(
    name = "cc_proto",
    srcs = glob(
        ["*.proto"],
    ),
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/v1:cc_proto",
    ],
)
