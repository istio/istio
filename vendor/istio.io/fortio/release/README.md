# How to make a fortio release

- Make sure `periodic/periodic.go`'s `Version` is newer than https://github.com/istio/fortio/releases

- Make a release there and document the changes since the previous release

- Make sure to use the same tag format (e.g "0.4.3" - note that there are no "v" in the tag to be consistent with the rest istio)

- Create the binary tgz: `make release` (from/in the toplevel directory)

- Upload the release/fortio-\*.tgz to GitHub


## How to change the build image

Update [../Dockerfile.build](../Dockerfile.build)

run
```
make update-build-image TAG=v5 DOCKER_PREFIX=fortio/fortio
```

Make sure it gets successfully pushed to the fortio/fortio registry

replace `v5` by whichever is the next one at the time

run
```
make update-build-image-tag TAG=v5
```
with same TAG as before

Check the diff and make lint, webtest, etc and PR
