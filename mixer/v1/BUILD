load("@com_google_protobuf//:protobuf.bzl", "cc_proto_library")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "attributes.pb.go",
        "check.pb.go",
        "report.pb.go",
        "service.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@io_istio_gogo_genproto//googleapis/google/rpc:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "attributes.proto",
        "check.proto",
        "report.proto",
        "service.proto",
    ],
    visibility = ["//visibility:public"],
)

filegroup(
    name = "attributes",
    srcs = ["attributes.proto"],
    visibility = ["//visibility:public"],
)

filegroup(
    name = "attributes_file",
    srcs = ["global_dictionary.yaml"],
    visibility = ["//visibility:public"],
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
        "//external:cc_gogoproto",
        "//external:cc_wkt_protos",
        "//external:rpc_status_proto",
    ],
)
