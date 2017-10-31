load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    testonly = 1,
    srcs = [
        "handler.go",
        "server.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/attribute:go_default_library",
        "//pkg/status:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@io_istio_api//:mixer/v1",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//codes:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["server_test.go"],
    library = ":go_default_library",
    deps = [
        "//pkg/attribute:go_default_library",
        "//pkg/status:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@io_istio_api//:mixer/v1",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
