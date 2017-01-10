package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "manager.go",
    ],
    deps = [
        "//pkg/aspect:go_default_library",
        "//pkg/aspect/logger:go_default_library",
        "//pkg/aspectsupport:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/expr:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_golang_protobuf//ptypes/struct:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    ],
)

go_test(
    name = "manager_test",
    size = "small",
    srcs = ["manager_test.go"],
    library = ":go_default_library",
    deps = [
        "@com_github_golang_protobuf//jsonpb/jsonpb_test_proto:go_default_library",
        "@com_github_istio_api//:mixer/v1/config",
    ],
)
