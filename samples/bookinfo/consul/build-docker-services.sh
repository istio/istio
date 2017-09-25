#!/bin/bash
#
# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# This script builds docker images for bookinfo microservices.
# It's different from ../src/build-services.sh because it builds all
# services with the envoy proxy and pilot agent in the images.
# Set required env vars. Ensure you have checked out the pilot project
SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
HUB=istio
VERSION=0.2.4
WORKSPACE=$GOPATH/src/istio.io/pilot
BINDIR=$WORKSPACE/bazel-bin
APPSDIR=$GOPATH/src/istio.io/istio/samples/bookinfo/src
PILOTAGENTPATH=$WORKSPACE/cmd/pilot-agent
PREPAREPROXYSCRIPT=$WORKSPACE/docker

# grab ISTIO_PROXY_BUCKET from pilot/WORKSPACE
ISTIO_PROXY_BUCKET=$(sed 's/ = /=/' <<< $( awk '/ISTIO_PROXY_BUCKET =/' $WORKSPACE/WORKSPACE))
PROXYVERSION=$(sed 's/[^"]*"\([^"]*\)".*/\1/' <<<  $ISTIO_PROXY_BUCKET)
# configure whether you want debug or not
PROXY=debug-$PROXYVERSION

set -x
set -o errexit

# Build the pilot agent binary
cd $PILOTAGENTPATH && bazel build :pilot-agent
STATUS=$?
if [ $STATUS -ne 0 ]; then
    echo -e "\n***********\nFAILED: build failed for pilot agent.\n***********\n"
    exit $STATUS
fi

cd $SCRIPTDIR

# Download the envoy proxy
echo "Download and extract the proxy: https://storage.googleapis.com/istio-build/proxy/envoy-$PROXY.tar.gz"
wget -qO- https://storage.googleapis.com/istio-build/proxy/envoy-$PROXY.tar.gz | tar xvz
cp usr/local/bin/envoy $APPSDIR/

# Copy the pilot agent binary to each app dir
# Build the images and  push them to hub
for app in details productpage ratings; do
  rm -f $APPSDIR/$app/pilot-agent && cp $BINDIR/cmd/pilot-agent/pilot-agent $_
  rm -f $APPSDIR/$app/prepare_proxy.sh && cp $PREPAREPROXYSCRIPT/prepare_proxy.sh $_
  rm -f $APPSDIR/$app/envoy && cp $APPSDIR/envoy $_
  docker build -f $APPSDIR/$app/Dockerfile.sidecar -t "$HUB/examples-bookinfo-${app}-v1-envoy:${VERSION}" $APPSDIR/$app/
  rm -f $APPSDIR/$app/pilot-agent $APPSDIR/$app/prepare_proxy.sh $APPSDIR/$app/envoy
done

REVIEWSDIR=$APPSDIR/reviews/reviews-wlpcfg

pushd $APPSDIR/reviews
    docker run --rm -v `pwd`:/usr/bin/app:rw niaquinto/gradle clean build
popd

rm -f $REVIEWSDIR/pilot-agent && cp $BINDIR/cmd/pilot-agent/pilot-agent $REVIEWSDIR
rm -f $REVIEWSDIR/prepare_proxy.sh && cp $PREPAREPROXYSCRIPT/prepare_proxy.sh $REVIEWSDIR
rm -f $REVIEWSDIR/envoy && cp $APPSDIR/envoy $REVIEWSDIR
#plain build -- no ratings
docker build -t $HUB/examples-bookinfo-reviews-v1-envoy:$VERSION --build-arg service_version=v1 \
    -f $APPSDIR/reviews/reviews-wlpcfg/Dockerfile.sidecar $APPSDIR/reviews/reviews-wlpcfg
#with ratings black stars
docker build -t $HUB/examples-bookinfo-reviews-v2-envoy:$VERSION --build-arg service_version=v2 \
    --build-arg enable_ratings=true -f $APPSDIR/reviews/reviews-wlpcfg/Dockerfile.sidecar $APPSDIR/reviews/reviews-wlpcfg
#with ratings red stars
docker build -t $HUB/examples-bookinfo-reviews-v3-envoy:$VERSION --build-arg service_version=v3 \
    --build-arg enable_ratings=true --build-arg star_color=red -f $APPSDIR/reviews/reviews-wlpcfg/Dockerfile.sidecar $APPSDIR/reviews/reviews-wlpcfg
rm -f $REVIEWSDIR/pilot-agent $REVIEWSDIR/prepare_proxy.sh $REVIEWSDIR/envoy

# clean up envoy downloaded artifacts
rm -rf $SCRIPTDIR/usr/local/bin/envoy $APPSDIR/envoy

# update the docker-compose.yaml file
sed -i.bak "s/image:\ \$HUB/image:\ $HUB/" docker-compose.yaml
