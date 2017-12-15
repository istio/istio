load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "monitoring.go",
        "server.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/adapter/config/aggregate:go_default_library",
        "//pilot/adapter/config/crd:go_default_library",
        "//pilot/adapter/config/file:go_default_library",
        "//pilot/adapter/config/ingress:go_default_library",
        "//pilot/adapter/config/memory:go_default_library",
        "//pilot/adapter/serviceregistry/aggregate:go_default_library",
        "//pilot/cmd:go_default_library",
        "//pilot/model:go_default_library",
        "//pilot/platform:go_default_library",
        "//pilot/platform/cloudfoundry:go_default_library",
        "//pilot/platform/consul:go_default_library",
        "//pilot/platform/eureka:go_default_library",
        "//pilot/platform/kube:go_default_library",
        "//pilot/platform/kube/admit:go_default_library",
        "//pilot/proxy:go_default_library",
        "//pilot/proxy/envoy:go_default_library",
        "//pilot/test/mock:go_default_library",
        "//pilot/tools/version:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_prometheus_client_golang//prometheus:go_default_library",
        "@com_github_prometheus_client_golang//prometheus/promhttp:go_default_library",
        "@io_istio_api//mesh/v1alpha1:go_default_library",
        "@io_k8s_client_go//kubernetes:go_default_library",
        "@org_cloudfoundry_code_copilot//:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = [
        "server_test.go",
    ],
    deps = [
        ":go_default_library",
        "//pilot/proxy/envoy:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
    ],
)
