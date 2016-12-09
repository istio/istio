load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_binary", "go_library", "go_test")

go_prefix("istio.io/manager")

go_binary(
    name = "istioctl",
    srcs = ["cli/main/istioctl.go"],
    deps = [":cli"],
)

go_library(
    name = "cli",
    srcs = glob(
        ["cli/*.go"],
    ),
    deps = [
        "@github_com_spf13_cobra//:cobra",
    ],
)

go_library(
    name = "model",
    srcs = [
        "model/functions.go",
        "model/store.go",
    ],
    deps = [
        "@github_com_golang_protobuf//:proto",
        "@github_com_hashicorp_go_multierror//:go-multierror",
    ],
)

go_test(
    name = "model/store_test",
    srcs = ["model/store_test.go"],
    library = ":model",
    deps = [":test"],
)

go_library(
    name = "env",
    srcs = [
        "env/kube_config.go",
        "env/kube_consumer.go",
        "env/kube_registry.go",
    ],
    deps = [
        ":model",
        "@github_com_golang_protobuf//:jsonpb",
        "@github_com_golang_protobuf//:proto",
        "@github_com_hashicorp_go_multierror//:go-multierror",
        "@github_com_kubernetes_client_go//:kubernetes",
        "@github_com_kubernetes_client_go//:pkg/api",
        "@github_com_kubernetes_client_go//:pkg/api/errors",
        "@github_com_kubernetes_client_go//:pkg/api/meta",
        "@github_com_kubernetes_client_go//:pkg/api/unversioned",
        "@github_com_kubernetes_client_go//:pkg/api/v1",
        "@github_com_kubernetes_client_go//:pkg/apis/extensions/v1beta1",
        "@github_com_kubernetes_client_go//:pkg/runtime",
        "@github_com_kubernetes_client_go//:pkg/runtime/serializer",
        "@github_com_kubernetes_client_go//:rest",
        "@github_com_kubernetes_client_go//:tools/clientcmd",
    ],
)

go_binary(
    name = "kube_agent",
    srcs = ["env/main/kube_agent.go"],
)

filegroup(
    name = "kube_agent_srcs",
    srcs = [
        "env/main/kube_agent.go",
    ],
    visibility = ["//visibility:public"],
)

go_test(
    name = "env/kube_test",
    srcs = ["env/kube_test.go"],
    library = ":env",
    deps = [":test"],
)

go_library(
    name = "test",
    srcs = [
        "test/mock_config.pb.go",
        "test/mocks.go",
    ],
    deps = [
        ":model",
        "@github_com_golang_protobuf//:proto",
    ],
)
