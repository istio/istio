# gazelle:ignore
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "env.go",
        "logger.go",
        "manager.go",
        "registry.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/aspect:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/pool:go_default_library",
        "//pkg/status:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_opentracing_opentracing_go//:go_default_library",
        "@com_github_opentracing_opentracing_go//log:go_default_library",
        "@com_github_prometheus_client_golang//prometheus:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "e2e_configuration_test.go",
        "env_test.go",
        "manager_test.go",
        "registry_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//adapter/noopLegacy:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/template:go_default_library",
        "//template/sample:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
