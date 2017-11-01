load("@io_bazel_rules_docker//docker:docker.bzl", "docker_build")

def broker_docker_build(images, **kwargs):
    for image in images:
        docker_build(
            name = image['name'],
            base = image['base'],
            **kwargs
        )
