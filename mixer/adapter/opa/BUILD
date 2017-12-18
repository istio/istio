package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["opa.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/opa/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "//mixer/template/authorization:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_open_policy_agent_opa//ast:go_default_library",
        "@com_github_open_policy_agent_opa//rego:go_default_library",
        "@com_github_open_policy_agent_opa//storage:go_default_library",
        "@com_github_open_policy_agent_opa//storage/inmem:go_default_library",
        "@com_github_open_policy_agent_opa//util:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "opa_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/adapter/test:go_default_library",
    ],
)

go_test(
    name = "go_integration_test",
    size = "small",
    srcs = [
        "opa_integration_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/server:go_default_library",
        "//mixer/template:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)
