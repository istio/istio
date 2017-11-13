load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library", "gogo_proto_compile")

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
    name = "config_fixed",
    srcs = [":config_gen"],
    outs = ["fixed_cfg.pb.go"],
    cmd = "sed " +
          "-e 's/*google_protobuf.Struct/interface{}/g' " +
          "-e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' " +
          "$(location :config_gen) | $(location @org_golang_x_tools_imports//:goimports) > $@",
    message = "Applying overrides to cfg proto",
    tools = ["@org_golang_x_tools_imports//:goimports"],
)
