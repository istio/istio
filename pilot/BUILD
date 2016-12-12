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
        "model/registry.go",
    ],
    deps = [
        "@github_com_golang_protobuf//:proto",
        "@github_com_hashicorp_go_multierror//:go-multierror",
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
        "platform/kube/config.go",
        "platform/kube/consumer.go",
        "platform/kube/registry.go",
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
    srcs = ["platform/kube/main/kube_agent.go"],
)

filegroup(
    name = "kube_agent_srcs",
    srcs = [
        "platform/kube/main/kube_agent.go",
    ],
    visibility = ["//visibility:public"],
)

go_test(
    name = "kube_test",
    srcs = ["platform/kube/minikube_test.go"],
    library = ":kube",
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

genrule(
    name = "process_cfgeditor_content",
    srcs = glob([
        "contrib/cfgeditor/content/**/*",
    ]),
    outs = ["content.go"],
    cmd = "go-bindata -o $(OUTS) --prefix contrib/cfgeditor/content contrib/cfgeditor/content/...",
)

go_binary(
    name = "cfgeditor",
    srcs = [
        "contrib/cfgeditor/main.go",
        "content.go",
    ],
    deps = [
        "@github_com_spf13_cobra//:cobra",
    ],
)
