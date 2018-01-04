package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "redisquota.go",
    ],
    deps = [
        "//mixer/adapter/redisquota/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "//mixer/template/quota:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@github_com_go_redis_redis//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "redisquota_integration_test.go",
        "redisquota_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/server:go_default_library",
        "//mixer/template:go_default_library",
        "@com_github_alicebob_miniredis//:go_default_library",
        "@com_github_alicebob_miniredis//server:go_default_library",
        "@com_github_yuin_gopher_lua//:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)
