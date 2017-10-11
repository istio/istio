load("@io_bazel_rules_go//proto:go_proto_library.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    srcs = [
        "dest_policy.proto",
        "egress_rule.proto",
        "http_fault.proto",
        "ingress_rule.proto",
        "l4_fault.proto",
        "proxy_mesh.proto",
        "route_rule.proto",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_protobuf//ptypes/any:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
)
