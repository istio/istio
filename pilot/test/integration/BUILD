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
        "//platform/kube/inject:go_default_library",
        "//proxy/envoy:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_sync//errgroup:go_default_library",
        "@io_istio_api//:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
    ],
)

go_binary(
    name = "integration",
    data = [
        "app.yaml.tmpl",
        "ca.yaml.tmpl",
        "cert.crt",
        "cert.key",
        "config.yaml",
        "config-auth.yaml",
        "egress-proxy.yaml.tmpl",
        "ingress.yaml",
        "ingress-proxy.yaml.tmpl",
        "manager-discovery.yaml.tmpl",
        "mixer.yaml.tmpl",
        "rule-content-route.yaml.tmpl",
        "rule-default-route.yaml.tmpl",
        "rule-fault-injection.yaml.tmpl",
        "rule-weighted-route.yaml.tmpl",
        "//platform/kube:kubeconfig",
    ],
    library = ":go_default_library",
    tags = ["manual"],
)
