load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "ephemeral.go",
        "finder.go",
        "kinds.go",
        "queries.go",
        "resourceType.go",
        "snapshot.go",
        "util.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/config/store:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/log:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

