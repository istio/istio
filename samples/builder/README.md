# Sample builder

This folder contains docker image building logic for various samples, to consolidate things.
Note some images still user per-folder config, so this is not complete.

## Building for testing

To build all images and push them:

```bash
docker buildx bake --push
```

This will push to `localhost:5000` by default, which you can override with `HUB=localhost:5000`.
It will also build `linux/amd64,linux/arm64` which you can override with `PLATFORMS`.

You can also build a set of images instead of all of them:

```bash
docker buildx bake --push examples-helloworld-v1 tcp-echo-server
```

## Updating images

When updating images, increment the version for the image in the `tags` config.
You will also want to update the sample YAMLs

## Building official images

Set `HUB=docker.io/istio` for official image builds.
Its best to only do this once for each image to avoid accidentally mutating existing images.
