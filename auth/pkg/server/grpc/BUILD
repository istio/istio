load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "authenticator.go",
        "authorizer.go",
        "server.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/pki:go_default_library",
        "//pkg/pki/ca:go_default_library",
        "//proto:go_default_library",
        "@com_github_coreos_go_oidc//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//codes:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//metadata:go_default_library",
        "@org_golang_google_grpc//peer:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "authenticator_test.go",
        "authorizer_test.go",
        "server_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/pki:go_default_library",
        "//pkg/pki/ca:go_default_library",
        "//proto:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//codes:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//metadata:go_default_library",
        "@org_golang_google_grpc//peer:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
