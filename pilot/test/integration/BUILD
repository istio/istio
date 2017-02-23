load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "driver.go",
        "reachability.go",
        "routing.go",
    ],
    deps = [
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_sync//errgroup:go_default_library",
        "@com_github_spf13_pflag//:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//pkg/apis/meta/v1:go_default_library",
    ],
)

go_binary(
    name = "integration",
    data = [
        "app.yaml.tmpl",
        "app-proxy-manager-agent.yaml.tmpl",
        "egress-proxy.yaml.tmpl",
        "manager-discovery.yaml.tmpl",
        "mixer.yaml.tmpl",
        "policy-fault-injection.yaml.tmpl",
        "rule-content-route.yaml.tmpl",
        "rule-default-route.yaml.tmpl",
        "rule-weighted-route.yaml.tmpl",
        "//platform/kube:kubeconfig",
    ],
    library = ":go_default_library",
    tags = ["manual"],
)
