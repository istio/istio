# gazelle:ignore
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_test(
    name = "go_default_test_e2e",
    size = "small",
    srcs = [
        "e2e_report_test.go",
        "util.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/template:go_default_library",
        "//test/spyAdapter:go_default_library",
        "//test/template:go_default_library",
        "//test/template/report:go_default_library",
        "//test/testenv:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1",  # keep
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
        "@org_golang_google_grpc//:go_default_library",
    ],
)
