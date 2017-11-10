load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["app.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//tests/e2e/apps/hop/config:go_default_library",
        "//tests/e2e/framework:go_default_library",
        "//tests/util:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_google_uuid//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["app_test.go"],
    library = ":go_default_library",
    deps = [
        "//tests/e2e/apps/hop/config:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
