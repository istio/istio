package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "client.go",
        "svcctrl.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/svcctrl/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/metric:go_default_library",
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
    srcs = ["svcctrl_test.go"],
    library = ":go_default_library",
    deps = [
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@org_golang_google_api//servicecontrol/v1:go_default_library",
    ],
)
