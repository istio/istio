package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "apiserver.go",
        "compatStore.go",
        "fsstore.go",
        "kind.go",
        "manager.go",
        "redisstore.go",
        "runtime.go",
        "store.go",
        "validator.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/pool:go_default_library",
        "//pkg/status:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
        "@com_github_redis_client//pool:go_default_library",
        "@com_github_redis_client//redis:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "apiserver_test.go",
        "fsstore_test.go",
        "kind_test.go",
        "manager_test.go",
        "redisstore_test.go",
        "runtime_test.go",
        "store_test.go",
        "storetestutil.go",
        "validator_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/aspect/config:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
        "@com_github_mini_redis//:go_default_library",
    ],
)
