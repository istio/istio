load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")

# TODO: also rename the binary to istio-auth-node-agent
pkg_tar(
    name = "agent-bin",
    srcs = [
        "//security/cmd/node_agent",
    ],
    mode = "0755",
    package_dir = "/usr/local/istio/bin",
)

pkg_tar(
    name = "istio-systemd",
    srcs = ["istio-auth-node-agent.service"],
    mode = "644",
    package_dir = "/lib/systemd/system",
)

pkg_tar(
    name = "debian-data",
    extension = "tar.gz",
    deps = [
        ":agent-bin",
        ":istio-systemd",
    ],
)

pkg_deb(
    name = "istio-auth-node-agent",
    architecture = "amd64",
    built_using = "bazel",
    data = ":debian-data",
    description_file = "description",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-auth-node-agent",
    version = "0.2-dev",
)

pkg_deb(
    name = "istio-auth-node-agent-release",
    architecture = "amd64",
    built_using = "bazel",
    data = ":debian-data",
    description_file = "description",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-auth-node-agent",
    tags = ["manual"],
    version_file = "version",
)
