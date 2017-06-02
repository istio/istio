load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")

def debug_docker_build(images, **kwargs):
    common_tars = kwargs.pop('tars', [])

    for image in images:
        tars = image.pop('tars', [])
        tars.extend(common_tars)

        docker_build(
            name = image['name'],
            base = image['base'],
            tars = tars,
            **kwargs
        )
