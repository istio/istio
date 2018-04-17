#! /bin/bash
# To be run by ../Makefile as release/release.sh
set -x
set -e
# Release tgz Dockerfile is based on the normal docker one
cat Dockerfile release/Dockerfile.in > release/Dockerfile
docker build -f release/Dockerfile -t istio/fortio:release .
DOCKERID=$(docker create --name fortio_release istio/fortio:release x)
function cleanup {
  docker rm fortio_release
}
trap cleanup EXIT
set -o pipefail
# docker cp will create 2 level of dir if first one exists, make sure it doesnt
rm -f release/tgz/*.tgz
rmdir release/tgz || true
docker cp -a fortio_release:/tgz/ release/tgz
tar tvfz release/tgz/*.tgz
# then save the result 1 level up
mv release/tgz/*.tgz release/
rmdir release/tgz
