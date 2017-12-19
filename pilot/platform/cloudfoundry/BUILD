load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "controller.go",
        "serviceaccounts.go",
        "servicediscovery.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@in_gopkg_validator_v2//:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
        "@org_cloudfoundry_code_copilot//api:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "cloudfoundry_suite_test.go",
        "config_test.go",
        "controller_test.go",
        "mockcopilotclient_test.go",
        "servicediscovery_test.go",
    ],
    deps = [
        ":go_default_library",
        "//pilot/model:go_default_library",
        "@com_github_onsi_ginkgo//:go_default_library",
        "@com_github_onsi_ginkgo//extensions/table:go_default_library",
        "@com_github_onsi_gomega//:go_default_library",
        "@com_github_square_certstrap//pkix:go_default_library",
        "@org_cloudfoundry_code_copilot//api:go_default_library",
        "@org_cloudfoundry_code_copilot//testhelpers:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
