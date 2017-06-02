load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")
load("//docker:debug_docker.bzl", "debug_docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "pilot_tar",
    extension = "tar.gz",
    files = [
        "//cmd/pilot",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

pkg_tar(
    name = "app_tar",
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
    name = "config_tar",
    extension = "tar.gz",
    files = ["envoy"],
    mode = "0666",
    modes = {"envoy": "777"},
    package_dir = "/etc",
    tags = ["manual"],
)

pkg_tar(
    name = "prepare_proxy_tar",
    extension = "tar.gz",
    files = [
        "prepare_proxy.sh",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

debug_docker_build(
    debs = [
        "@deb_iptables//file",
        "@deb_libnfnetlink//file",
        "@deb_libxtables//file",
    ],
    entrypoint = ["/usr/local/bin/prepare_proxy.sh"],
    images = [
        {
            "name": "init",
            "base": "@docker_ubuntu//:xenial",
        },
        {
            "name": "init_debug",
            "base": "@ubuntu_xenial_debug//file",
        },
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [":prepare_proxy_tar"],
)

debug_docker_build(
    entrypoint = ["/usr/local/bin/pilot"],
    images = [
        {
            "name": "proxy",
            "base": "@docker_ubuntu//:xenial",
            "tars": [
                "@istio_proxy//file",
            ],
        },
        {
            "name": "proxy_debug",
            "base": "@ubuntu_xenial_debug//file",
            "tars": [
                "@istio_proxy_debug//file",
            ],
        },
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":pilot_tar",
        ":config_tar",
    ],
)

docker_build(
    name = "pilot",
    base = "@docker_ubuntu//:xenial",
    entrypoint = ["/usr/local/bin/pilot"],
    repository = "istio",
    tags = ["manual"],
    tars = [":pilot_tar"],
)

debug_docker_build(
    entrypoint = ["/bin/server"],
    images = [
        {
            "name": "app",
            "base": "@docker_ubuntu//:xenial",
        },
        {
            "name": "app_debug",
            "base": "@ubuntu_xenial_debug//file",
        },
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":app_tar",
    ],
)
