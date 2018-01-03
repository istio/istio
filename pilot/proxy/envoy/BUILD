load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "discovery.go",
        "fault.go",
        "header.go",
        "infra_auth.go",
        "ingress.go",
        "mixer.go",
        "policy.go",
        "resources.go",
        "route.go",
        "watcher.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/model:go_default_library",
        "//pilot/proxy:go_default_library",
        "//pilot/tools/version:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",

        # TODO(nmittler): Remove this
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_howeyc_fsnotify//:go_default_library",
        "@com_github_prometheus_client_golang//prometheus:go_default_library",
        "@io_istio_api//mesh/v1alpha1:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@io_istio_api//mixer/v1/config/client:go_default_library",
        "@io_istio_api//routing/v1alpha1:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "config_test.go",
        "discovery_test.go",
        "header_test.go",
        "infra_auth_test.go",
        "ingress_test.go",
        "route_test.go",
        "watcher_test.go",
    ],
    data = glob(["testdata/*.golden"]) + [
        ":envoy_binary",
    ],
    library = ":go_default_library",
    deps = [
        "//pilot/adapter/config/memory:go_default_library",
        "//pilot/model:go_default_library",
        "//pilot/proxy:go_default_library",
        "//pilot/test/mock:go_default_library",
        "//pilot/test/util:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_howeyc_fsnotify//:go_default_library",
        "@io_istio_api//mesh/v1alpha1:go_default_library",
        "@io_istio_api//routing/v1alpha1:go_default_library",
    ],
)

genrule(
    name = "envoy_binary",
    srcs = ["@envoy_binary//file"],
    outs = ["envoy"],
    cmd = "tar xzf $(location @envoy_binary//file); mv usr/local/bin/envoy $@",
)
