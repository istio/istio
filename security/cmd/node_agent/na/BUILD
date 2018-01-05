load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "nafactory.go",
        "nodeagent.go",
        "util.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/log:go_default_library",
        "//security/pkg/pki:go_default_library",
        "//security/pkg/pki/ca:go_default_library",
        "//security/pkg/platform:go_default_library",
        "//security/pkg/workload:go_default_library",
        "//security/proto:go_default_library",
        # TODO(nmittler): Remove this
        "@com_github_golang_glog//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "config_test.go",
        "nafactory_test.go",
        "nodeagent_test.go",
        "util_test.go",
    ],
    data = glob(["testdata/*"]),
    library = ":go_default_library",
    deps = [
        "//pkg/log:go_default_library",
        "//security/pkg/platform:go_default_library",
        "//security/pkg/platform/mock:go_default_library",
        "//security/pkg/util/mock:go_default_library",
        "//security/pkg/workload:go_default_library",
        "//security/proto:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//reflection:go_default_library",
        "@org_golang_x_net//context:go_default_library",
        "@io_istio_gogo_genproto//googleapis/google/rpc:go_default_library",
    ],
)
