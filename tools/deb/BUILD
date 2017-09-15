load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")

# TODO: decide the proper location for binaries and configs and update the file.
# Current layout for binaries matches 0.1 and docker images.

pkg_tar(
    name = "envoy-bin",
    files = [
        "istio-iptables.sh",
        "istio-start.sh",
        "//src/envoy/mixer:envoy",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin",
)

pkg_tar(
    name = "istio-conf",
    files = [
        "envoy.json",
        "sidecar.env",
    ],
    mode = "0755",
    package_dir = "/var/lib/istio/envoy",
)

pkg_tar(
    name = "istio-systemd",
    files = ["istio.service"],
    mode = "644",
    package_dir = "/lib/systemd/system",
)

pkg_tar(
    name = "debian-data",
    extension = "tar.gz",
    deps = [
        ":envoy-bin",
        ":istio-conf",
        ":istio-systemd",
    ],
)

pkg_deb(
    name = "istio-proxy",
    architecture = "amd64",
    built_using = "bazel",
    data = ":debian-data",
    description_file = "description",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-proxy",
    postinst = "postinst.sh",
    version = "0.2-dev",
)

pkg_deb(
    name = "istio-proxy-release",
    architecture = "amd64",
    built_using = "bazel",
    data = ":debian-data",
    description_file = "description",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-proxy",
    postinst = "postinst.sh",
    tags = ["manual"],
    version_file = "version",
)
