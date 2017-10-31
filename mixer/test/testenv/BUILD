package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["testenv.go"],
    deps = [
        "//mixer/cmd/server/cmd:go_default_library",
        "//mixer/cmd/shared:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "@io_istio_api//:mixer/v1",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["testenv_test.go"],
    data = [
        "//mixer/testdata",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter/denier:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template:go_default_library",
        "@com_github_pborman_uuid//:go_default_library",
        "@io_istio_api//:mixer/v1",
        "@org_golang_x_net//context:go_default_library",
    ],
)
