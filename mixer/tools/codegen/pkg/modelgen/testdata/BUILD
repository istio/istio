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
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "BasicTopLevelFields.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "constructor_and_imports",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
        "external/com_github_istio_api",
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_istio_api//:mixer/v1/config/descriptor_protos",
    ],
    protos = [
        "ConstructorAndImports.proto",
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
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingBothRequiredExt.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_constructor_message",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingConstructorMessage.proto",
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
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingPackageName.proto",
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
    ],
    inputs = [
        "//pkg/adapter/template:protos",
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
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingTemplateVarietyExt.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "missing_type_message",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "MissingTypeMessage.proto",
    ],
    verbose = 0,
)

proto_compile(
    name = "reserved_field_in_constructor",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "ReservedFieldInConstructor.proto",
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
    ],
    inputs = [
        "//pkg/adapter/template:protos",
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
        "constructor_and_imports.descriptor_set",
        "missing_both_required.descriptor_set",
        "missing_constructor_message.descriptor_set",
        "missing_package_name.descriptor_set",
        "missing_template_name.descriptor_set",
        "missing_template_variety.descriptor_set",
        "missing_type_message.descriptor_set",
        "proto2_bad_syntax.descriptor_set",
        "reserved_field_in_constructor.descriptor_set",
    ],
)
