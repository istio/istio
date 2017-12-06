# gazelle:ignore
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library", "gogo_proto_compile")

gogo_proto_compile(
    name = "rbac_gen",
    importmap = {
        "mixer/v1/config/descriptor/value_type.proto": "istio.io/api/mixer/v1/config/descriptor",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//mixer/v1/config/descriptor:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "rbac.proto",
    ],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
)

genrule(
    name = "rbac_fixed",
    srcs = [":rbac_gen"],
    outs = ["fixed_rbac.pb.go"],
    cmd = "sed " +
          "-e 's/*google_protobuf.Struct/interface{}/g' " +
          "-e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' " +
          "$(location :rbac_gen) | $(location @org_golang_x_tools_imports//:goimports) > $@",
    message = "Applying overrides to rbac proto",
    tools = ["@org_golang_x_tools_imports//:goimports"],
    visibility = ["//visibility:public"],
)

go_library(
    name = "go_default_library",
    srcs = [
        "combined.go",
        "@io_istio_api//mixer/v1/config:config_fixed",  # keep
        "rbac_fixed",
    ],
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:descriptor",  # keep
    ],
)

