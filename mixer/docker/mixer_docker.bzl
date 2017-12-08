load("@io_bazel_rules_docker//docker:docker.bzl", "docker_build")

def mixer_docker_ubuntu_build(images, **kwargs):
    for image in images:
        docker_build(
            name = image['name'],
            base = image['base'],
            **kwargs
        )

def mixer_docker_fedora_build(images, **kwargs):
    for image in images:
        docker_build(
            name = image['name'],
            base = image['base'],
            **kwargs
        )
