load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogo_proto_compile")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

# gogoslick_proto_compile cannot be used here. it generates Equal, Size, and
# MarshalTo methods for google.protobuf.Struct, which we then later replace
# with interface{}. This causes compilation issues.
gogo_proto_compile(
    name = "config_gen",
    importmap = {
        "google/protobuf/struct.proto": "github.com/gogo/protobuf/types",
        "mixer/v1/config/descriptor/log_entry_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/metric_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/monitored_resource_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/principal_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/quota_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/value_type.proto": "istio.io/api/mixer/v1/config/descriptor",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = [
        "//mixer/v1/config/descriptor:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "cfg.proto",
    ],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
)

genrule(
    name = "mixer/v1/config_fixed",
    srcs = [":config_gen"],
    outs = ["fixed_cfg.pb.go"],
    cmd = "sed " +
          "-e 's/*google_protobuf.Struct/interface{}/g' " +
          "-e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' " +
          "$(location :config_gen) | $(location @org_golang_x_tools_imports//:goimports) > $@",
    message = "Applying overrides to cfg proto",
    tools = ["@org_golang_x_tools_imports//:goimports"],
)

go_library(
    name = "go_default_library",
    visibility = ["//visibility:public"],
    srcs = ["fixed_cfg.pb.go"],
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
        "//mixer/v1/config/descriptor:go_default_library",
    ],
)
