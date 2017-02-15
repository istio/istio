load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "manager",
    extension = "tar.gz",
    files = [
        "//cmd/manager",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

pkg_tar(
    name = "test",
    extension = "tar.gz",
    files = [
        "//test/client",
        "//test/server",
    ],
    mode = "0755",
    package_dir = "/bin",
    tags = ["manual"],
)

pkg_tar(
    name = "config",
    extension = "tar.gz",
    files = ["envoy"],
    mode = "0666",
    modes = {"envoy": "777"},
    package_dir = "/etc",
    tags = ["manual"],
)

pkg_tar(
    name = "prepare_proxy",
    extension = "tar.gz",
    files = [
        "prepare_proxy.sh",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

docker_build(
    name = "init",
    base = "@docker_ubuntu//:xenial",
    debs = [
        "@deb_iptables//file",
        "@deb_libnfnetlink//file",
        "@deb_libxtables//file",
    ],
    entrypoint = ["/usr/local/bin/prepare_proxy.sh"],
    repository = "istio",
    tags = ["manual"],
    tars = [":prepare_proxy"],
    visibility = ["//visibility:public"],
)

docker_build(
    name = "runtime",
    base = "@docker_ubuntu//:xenial",
    entrypoint = ["/usr/local/bin/manager"],
    repository = "istio",
    tags = ["manual"],
    tars = [
        "@istio_proxy//file",
        ":manager",
        ":config",
    ],
    visibility = ["//visibility:public"],
)

docker_build(
    name = "app",
    base = "@docker_ubuntu//:xenial",
    entrypoint = ["/bin/server"],
    repository = "istio",
    tags = ["manual"],
    tars = [":test"],
    visibility = ["//visibility:public"],
)
