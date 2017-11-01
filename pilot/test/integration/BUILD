load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "access.go",
        "driver.go",
        "egress_rules.go",
        "grpc.go",
        "headless.go",
        "http.go",
        "infra.go",
        "ingress.go",
        "routing.go",
        "tcp.go",
        "zipkin.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//pilot/adapter/config/crd:go_default_library",
        "//pilot/model:go_default_library",
        "//pilot/platform:go_default_library",
        "//pilot/platform/kube:go_default_library",
        "//pilot/platform/kube/inject:go_default_library",
        "//pilot/test/util:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_sync//errgroup:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_satori_go_uuid//:go_default_library",
        "@io_istio_api//:proxy/v1/config",
        "@io_k8s_api//core/v1:go_default_library",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
    ],
)

go_binary(
    name = "integration",
    data = glob(["testdata/*"]) + [
        "//pilot/platform/kube:kubeconfig",
        "//docker:certs",
    ],
    library = ":go_default_library",
    tags = ["manual"],
    visibility = ["//visibility:public"],
)
