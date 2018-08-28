# How to make a fortio release

- Make sure `version/version.go`'s `major`/`minor`/`patch` is newer than the most recent [release](https://github.com/fortio/fortio/releases)

- Make a release there and document the changes since the previous release

- Make sure to use the same git tag format (e.g "v0.7.1" - note that there is `v` prefix in the tag, like many projects but unlike the rest of istio). Docker and internal version/tag is "0.7.1", the `v` is only for git tags.

- Make sure your git status is clean, and the tag is present before the next step or it will get marked dirty/pre

- Create the binary tgz: `make release` (from/in the toplevel directory)

- Upload the release/fortio-\*.tgz to GitHub

- The docker official builds are done automatically based on tag, check [fortio's cloud docker build page](https://cloud.docker.com/app/fortio/repository/docker/fortio/fortio/builds)

- Increment the `patch` and commit that right away so the first point is true next time and so master/latest docker images have the correct next-pre version.

- Once the release is deemed good/stable: move the git tag `latest_release` to the same as the release.

  ```Shell
  # for instance for 0.11.0:
  git fetch
  git checkout v0.11.0
  git tag -f latest_release
  git push -f --tags
  ```

- Also push `latest_release` docker tag/image: wait for the autobuild to make it and then:

  ```Shell
  # for instance for 0.11.0:
  docker image pull fortio/fortio:1.1.1
  docker tag fortio/fortio:1.1.1 fortio/fortio:latest_release
  docker push fortio/fortio:latest_release
  ```

- To update the command line flags in the ../README.md; go install the right version of fortio so it is in your path and run updateFlags.sh

- Update the homebrew tap

## How to change the build image

Update [../Dockerfile.build](../Dockerfile.build)

Edit the `BUILD_IMAGE_TAG := v5` line in the Makefile, set it to `v6`
for instance (replace `v6` by whichever is the next one at the time)

run

```Shell
make update-build-image
```

Make sure it gets successfully pushed to the fortio registry (requires org access)

run

```Shell
make update-build-image-tag
```

Check the diff and make lint, webtest, etc and PR
