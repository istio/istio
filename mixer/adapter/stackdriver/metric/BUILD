package(default_visibility = ["//adapter/stackdriver:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "bufferedClient.go",
        "merge.go",
        "metric.go",
    ],
    deps = [
        "//adapter/stackdriver/config:go_default_library",
        "//adapter/stackdriver/helper:go_default_library",
        "//pkg/adapter:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
        "@com_github_googleapis_gax_go//:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
        "@com_google_cloud_go//monitoring/apiv3:go_default_library",
        "@org_golang_google_genproto//googleapis/api/label:go_default_library",
        "@org_golang_google_genproto//googleapis/api/metric:go_default_library",
        "@org_golang_google_genproto//googleapis/api/monitoredres:go_default_library",
        "@org_golang_google_genproto//googleapis/monitoring/v3:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "bufferedClient_test.go",
        "merge_test.go",
        "metric_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//adapter/stackdriver/config:go_default_library",
        "//pkg/adapter/test:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
        "@com_github_googleapis_gax_go//:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
        "@com_google_cloud_go//monitoring/apiv3:go_default_library",
        "@org_golang_google_genproto//googleapis/api/metric:go_default_library",
        "@org_golang_google_genproto//googleapis/api/monitoredres:go_default_library",
        "@org_golang_google_genproto//googleapis/monitoring/v3:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
