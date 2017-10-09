package(default_visibility = ["//tools/codegen/pkg/modelgen:__pkg__"])

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_compile")

proto_compile(
    name = "basic_top_level_fields",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "BasicTopLevelFields.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "simple_template",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@io_istio_api//:mixer/v1/config/descriptor_protos",  # keep
    ],
    protos = [
        "SimpleTemplate.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "unsupported_field_type_message",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@io_istio_api//:mixer/v1/config/descriptor_protos",  # keep
    ],
    protos = [
        "UnsupportedFieldTypeMessage.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "unsupported_field_type_as_map",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@io_istio_api//:mixer/v1/config/descriptor_protos",  # keep
    ],
    protos = [
        "UnsupportedFieldTypeAsMap.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "unsupported_field_type_primitive",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@io_istio_api//:mixer/v1/config/descriptor_protos",  # keep
    ],
    protos = [
        "UnsupportedFieldTypePrimitive.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "unsupported_field_type_enum",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@io_istio_api//:mixer/v1/config/descriptor_protos",  # keep
    ],
    protos = [
        "UnsupportedFieldTypeEnum.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_both_required",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingBothRequiredExt.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_package_name",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingPackageName.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "wrong_pkg_name",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "WrongPkgName.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_template_name",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingTemplateNameExt.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_template_variety",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingTemplateVarietyExt.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_template_message",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingTemplateMessage.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "reserved_field_in_template",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "ReservedFieldInTemplate.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "proto2_bad_syntax",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/io_istio_api",
    ],
    inputs = [
        "@io_istio_api//:mixer/v1/template_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "Proto2BadSyntax.proto",
    ],
    verbose = 0,
)

filegroup(
    name = "test_descriptors",
    testonly = True,
    srcs = [
        "basic_top_level_fields.descriptor_set",
        "missing_both_required.descriptor_set",
        "missing_package_name.descriptor_set",
        "missing_template_message.descriptor_set",
        "missing_template_variety.descriptor_set",
        "proto2_bad_syntax.descriptor_set",
        "reserved_field_in_template.descriptor_set",
        "simple_template.descriptor_set",
        "unsupported_field_type_as_map.descriptor_set",
        "unsupported_field_type_enum.descriptor_set",
        "unsupported_field_type_message.descriptor_set",
        "unsupported_field_type_primitive.descriptor_set",
        "wrong_pkg_name.descriptor_set",
    ],
)
