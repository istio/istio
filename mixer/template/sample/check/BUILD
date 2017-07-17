package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_compile")

go_proto_compile(
    name = "go_compiled_src",
    importmap = {
        "google/protobuf/descriptor.proto": "github.com/golang/protobuf/protoc-gen-go/descriptor",
        "google/protobuf/duration.proto": "github.com/golang/protobuf/ptypes/duration",
        "mixer/v1/config/descriptor/value_type.proto": "istio.io/api/mixer/v1/config/descriptor",
        "google/protobuf/descriptor.proto": "github.com/golang/protobuf/protoc-gen-go/descriptor",
        "google/protobuf/struct.proto": "github.com/golang/protobuf/ptypes/struct",
        "pkg/template/mixer/TemplateExtensions.proto": "istio.io/mixer/pkg/template/mixer",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "external/com_github_istio_api",
    ],
    inputs = [
        "//pkg/template/mixer:protos",
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_istio_api//:mixer/v1/config/descriptor_protos",
    ],
    protos = ["CheckTesterTemplate.gen.proto"],
)

genrule(
    name = "fix_go_compiled_src",
    srcs = [
        ":go_compiled_src",
    ],
    outs = ["CheckTesterTemplate.gen.altered.pb.go"],
    cmd = "sed -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' $(locations :go_compiled_src) > $@",
)

go_library(
    name = "go_default_library",
    srcs = [
        "CheckTesterProcessor.gen.go",
        ":fix_go_compiled_src",
    ],
    deps = [
        "//pkg/adapter/config:go_default_library",
        "//pkg/template/mixer:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)
