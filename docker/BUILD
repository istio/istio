load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@io_bazel_rules_docker//docker:docker.bzl", "docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "hop_tar",
    extension = "tar.gz",
    srcs = [
        "//tests/e2e/apps/hop/hop-server",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin",
    tags = ["manual"],
)

docker_build(
    name = "hop",
    base = "@docker_ubuntu//:xenial",
    entrypoint = ["/usr/local/bin/hop-server"],
    repository = "istio-testing",
    tags = ["manual"],
    tars = ["hop_tar"],
)
