load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "gcp.go",
        "aws.go",
        "nafactory.go",
        "nodeagent.go",
        "onprem.go",
        "util.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/credential:go_default_library",
        "//pkg/pki:go_default_library",
        "//pkg/pki/ca:go_default_library",
        "//pkg/util:go_default_library",
        "//pkg/workload:go_default_library",
        "//proto:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_google_cloud_go//compute/metadata:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_x_net//context:go_default_library",
        "@com_github_aws_aws-sdk-go//aws/ec2metadata:go_default_library",
        "@com_github_aws_aws-sdk-go//aws/session:go_default_library",
        "@com_github_fullsailor_pkcs7//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "config_test.go",
        "gcp_test.go",
        "nafactory_test.go",
        "nodeagent_test.go",
        "onprem_test.go",
        "util_test.go",
        "aws_test.go",
    ],
    data = glob(["testdata/*"]),
    library = ":go_default_library",
    deps = [
        "//pkg/util/mock:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//reflection:go_default_library",
        "@com_github_aws_aws-sdk-go//aws:go_default_library",
        "@com_github_aws_aws-sdk-go//aws/ec2metadata:go_default_library",
        "@com_github_aws_aws-sdk-go//awstesting/unit:go_default_library",
    ],
)
