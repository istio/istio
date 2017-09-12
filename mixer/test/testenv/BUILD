package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test", "go_binary")

go_library(
    name = "go_default_library",
    srcs = [
        "testenv.go",
    ],
    deps = [
        "//cmd/server/cmd:go_default_library",
        "//cmd/shared:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/template:go_default_library",
        "@com_github_istio_api//:mixer/v1",  # keep
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["testenv_test.go"],
    data = [
        "//testdata",
    ],
    library = ":go_default_library",
    deps = [
        "//adapter/denier:go_default_library",
        "//pkg/adapter:go_default_library",
        "//template:go_default_library",
        "//test/testenv:go_default_library",
        "@com_github_istio_api//:mixer/v1",  # keep
        "@com_github_pborman_uuid//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
