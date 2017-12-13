package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "checkprocessor.go",
        "client.go",
        "distValueBuilder.go",
        "handler.go",
        "metrics.go",
        "quotaprocessor.go",
        "reportbuilder.go",
        "reportprocessor.go",
        "servicecontrol.go",
        "testhelper.go",
        "utils.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/servicecontrol/config:go_default_library",
        "//mixer/adapter/servicecontrol/template/servicecontrolreport:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/cache:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "//mixer/template/apikey:go_default_library",
        "//mixer/template/quota:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_pborman_uuid//:go_default_library",
        "@org_golang_google_api//servicecontrol/v1:go_default_library",
        "@org_golang_x_net//context:go_default_library",
        "@org_golang_x_oauth2//:go_default_library",
        "@org_golang_x_oauth2//google:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "checkprocessor_test.go",
        "distValueBuilder_test.go",
        "handler_test.go",
        "quotaprocessor_test.go",
        "reportbuilder_test.go",
        "reportprocessor_test.go",
        "servicecontrol_test.go",
        "testhelper.go",
        "utils_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter/servicecontrol/config:go_default_library",
        "//mixer/adapter/servicecontrol/template/servicecontrolreport:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "//mixer/template/apikey:go_default_library",
        "//mixer/template/quota:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@org_golang_google_api//googleapi:go_default_library",
        "@org_golang_google_api//servicecontrol/v1:go_default_library",
    ],
)
