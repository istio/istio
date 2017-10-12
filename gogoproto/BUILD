package(default_visibility = ["//visibility:public"])

proto_library(
    name = "gogoproto_protos",
    srcs = [":gogo.proto"],
    deps = [
        "@com_google_protobuf//:any_proto",
        "@com_google_protobuf//:descriptor_proto",
    ],
)

genrule(
    name = "status_proto",
    srcs = ["@com_github_gogo_protobuf//gogoproto:go_default_library_protos"],
    outs = ["gogo.proto"],
    cmd = "cat $(location @com_github_gogo_protobuf//gogoproto:go_default_library_protos) > $@",
)
