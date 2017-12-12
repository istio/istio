load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "resourceType.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        # Only for the dispatcher interface.
        "//mixer/pkg/runtime:go_default_library",

        "//mixer/pkg/il/compiled:go_default_library",
        "//mixer/pkg/template:go_default_library",
#        "@com_github_gogo_protobuf//proto:go_default_library",
#        "@com_github_golang_glog//:go_default_library",
#        "@com_github_googleapis_googleapis//:google/rpc",
#        "@com_github_hashicorp_go_multierror//:go_default_library",
#        "@com_github_opentracing_opentracing_go//:go_default_library",
#        "@com_github_opentracing_opentracing_go//log:go_default_library",
#        "@com_github_prometheus_client_golang//prometheus:go_default_library",
#        "@io_istio_api//mixer/v1/config/descriptor:descriptor",
#        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

