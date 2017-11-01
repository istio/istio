load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "aws.go",
        "client.go",
        "gcp.go",
        "onprem.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//auth/pkg/credential:go_default_library",
        "//auth/pkg/pki:go_default_library",
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
        "aws_test.go",
        "gcp_test.go",
        "onprem_test.go",
    ],
    data = glob(["testdata/*"]),
    library = ":go_default_library",
    deps = [
        "@com_github_aws_aws-sdk-go//aws:go_default_library",
        "@com_github_aws_aws-sdk-go//awstesting/unit:go_default_library",
    ],
)
