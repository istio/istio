package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "accessLogsManager.go",
        "apiMethod.go",
        "applicationLogsManager.go",
        "denialsManager.go",
        "descriptors.go",
        "inventory.go",
        "kind.go",
        "listsManager.go",
        "manager.go",
        "metricsManager.go",
        "quotasManager.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/aspect/config:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/pool:go_default_library",
        "//pkg/status:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "accessLogsManager_test.go",
        "applicationLogsManager_test.go",
        "denialsManager_test.go",
        "descriptors_test.go",
        "listsManager_test.go",
        "metricsManager_test.go",
        "quotasManager_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter/test:go_default_library",
        "//pkg/aspect/test:go_default_library",
    ],
)
