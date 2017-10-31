# TODO: Convert to a gazelle friendly go_binary.name and remove "ignore"
# gazelle:ignore

load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("//mixer/docker:mixer_docker.bzl", "mixer_docker_build")
load("//mixer/docker:cacerts.bzl", "cacerts")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "mixer_tar",
    extension = "tar.gz",
    files = [
        "//mixer/cmd/server:mixs",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

sh_binary(
    name = "extract_certs",
    srcs = ["extract.sh"],
)

cacerts(
    name = "cacerts",
)

mixer_docker_build(
    cmd = [
        "--configStoreURL=fs:///etc/opt/mixer/configroot",
        "--configStore2URL=k8s://",
        "--logtostderr",
        "--v=2",
        "--traceOutput=http://zipkin:9411/api/v1/spans",
    ],
    debs = [
        "@openssl//file:pkg.deb",
        "@libssl1.0.0//file:pkg.deb",
    ],
    entrypoint = [
        "/usr/local/bin/mixs",
        "server",
    ],
    images = [
        {
            "name": "mixer",
            "base": "@docker_ubuntu//:xenial",
        },
        {
            "name": "mixer_debug",
            "base": "@ubuntu_xenial_debug//file",
        },
    ],
    ports = [
        "9091",
        "9093",
        "9094",
        "42422",
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":mixer_tar",
        "//mixer/testdata:configs_tar",
        ":cacerts.tar",
    ],
)
