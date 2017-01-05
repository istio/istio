package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "adapter.go",
        "aspect.go",
    ],
    deps = [
        ":config_proto",
        "//pkg/aspect:go_default_library",
        "//pkg/aspect/denyChecker:go_default_library",
        "//pkg/registry:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
    ],
)

go_test(
    name = "deny_checker_test",
    size = "small",
    srcs = ["adapter_test.go"],
    library = ":go_default_library",
    deps = ["//pkg/adaptertesting:go_default_library"],
)

load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "config_proto",
    importmap = {
        "google/rpc/status.proto": "google.golang.org/genproto/googleapis/rpc/status",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "external/com_github_googleapis_googleapis",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_googleapis_googleapis//:status_proto",
    ],
    protos = [
        "config.proto",
    ],
    verbose = 0,
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
        "@com_github_googleapis_googleapis//:go_status_proto",
    ],
)
