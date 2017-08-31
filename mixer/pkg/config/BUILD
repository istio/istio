package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "adapterInfoRegistry.go",
        "apiserver.go",
        "compatStore.go",
        "handler.go",
        "inventory.go",
        "kind.go",
        "manager.go",
        "runtime.go",
        "validator.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config/crd:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/config/redis:go_default_library",
        "//pkg/config/store:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/handler:go_default_library",
        "//pkg/pool:go_default_library",
        "//pkg/status:go_default_library",
        "//pkg/template:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "adapterInfoRegistry_test.go",
        "apiserver_test.go",
        "handler_test.go",
        "kind_test.go",
        "manager_test.go",
        "runtime_test.go",
        "validator_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/aspect/config:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/config/store:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/handler:go_default_library",
        "//pkg/template:go_default_library",
        "//template/sample:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
