load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "discovery.go",
        "egress.go",
        "fault.go",
        "header.go",
        "ingress.go",
        "mixer.go",
        "policy.go",
        "resources.go",
        "route.go",
        "watcher.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "//proxy:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_howeyc_fsnotify//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "config_test.go",
        "discovery_test.go",
        "header_test.go",
        "ingress_test.go",
        "route_test.go",
        "watcher_test.go",
    ],
    data = glob(["testdata/*.golden"]) + [
        ":envoy_binary",
    ],
    library = ":go_default_library",
    deps = [
        "//adapter/config/memory:go_default_library",
        "//model:go_default_library",
        "//proxy:go_default_library",
        "//test/mock:go_default_library",
        "//test/util:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_howeyc_fsnotify//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

genrule(
    name = "envoy_binary",
    srcs = ["@envoy_binary//file"],
    outs = ["envoy"],
    cmd = "tar xzf $(location @envoy_binary//file); mv usr/local/bin/envoy $@",
)
