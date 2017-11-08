package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["circonus.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/circonus/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/metric:go_default_library",

        "@com_github_circonuslabs_circonus_gometrics//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_pkg_errors//:go_default_library",
        "@com_github_tv42_httpunix//:go_default_library",

        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["circonus_test.go"],
    library = ":go_default_library",
    data = [
        "operatorconfig",
    ],
    deps = [
        "//mixer/test/testenv:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/template:go_default_library",
        "@com_github_pborman_uuid//:go_default_library",
        "@io_istio_api//:mixer/v1",
        "@org_golang_x_net//context:go_default_library",
    ],
)
