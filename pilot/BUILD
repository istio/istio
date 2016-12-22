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
        "@com_github_spf13_cobra//:go_default_library",
    ],
)

go_library(
    name = "model",
    srcs = [
        "model/registry.go",
        "model/validation.go",
    ],
    deps = [
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "model/registry_test",
    srcs = ["model/registry_test.go"],
    library = ":model",
    deps = [":test"],
)

go_library(
    name = "kube",
    srcs = [
        "platform/kube/client.go",
        "platform/kube/config.go",
        "platform/kube/consumer.go",
        "platform/kube/controller.go",
        "platform/kube/queue.go",
    ],
    deps = [
        ":model",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//pkg/api:go_default_library",
        "@io_k8s_client_go//pkg/api/errors:go_default_library",
        "@io_k8s_client_go//pkg/api/meta:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//pkg/apis/extensions/v1beta1:go_default_library",
        "@io_k8s_client_go//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//pkg/runtime:go_default_library",
        "@io_k8s_client_go//pkg/runtime/schema:go_default_library",
        "@io_k8s_client_go//pkg/runtime/serializer:go_default_library",
        "@io_k8s_client_go//pkg/util/flowcontrol:go_default_library",
        "@io_k8s_client_go//pkg/watch:go_default_library",
        "@io_k8s_client_go//rest:go_default_library",
        "@io_k8s_client_go//tools/cache:go_default_library",
        "@io_k8s_client_go//tools/clientcmd:go_default_library",
    ],
)

go_binary(
    name = "kube_agent",
    srcs = ["platform/kube/main/kube_agent.go"],
    visibility = ["//visibility:public"],
)

go_test(
    name = "kube_test",
    srcs = ["platform/kube/minikube_test.go"],
    data = [
        "platform/kube/config",
    ],
    library = ":kube",
    deps = [":test"],
)

go_test(
    name = "queue_test",
    srcs = ["platform/kube/queue_test.go"],
    library = ":kube",
)

go_library(
    name = "test",
    srcs = [
        "test/mock_config.pb.go",
        "test/mocks.go",
    ],
    deps = [
        ":model",
        "@com_github_golang_protobuf//proto:go_default_library",
    ],
)

go_test(
    name = "test/mocks_test",
    srcs = ["test/mocks_test.go"],
    library = ":test",
    deps = [":model"],
)
