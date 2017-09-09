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

# Set required env vars. Ensure you have checked out the pilot project
SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
HUB=${whichhub}
WORKSPACE=$GOPATH/src/istio.io/pilot
BINDIR=$WORKSPACE/bazel-bin
APPSDIR=$SCRIPTDIR
DISCOVERYDIR=$SCRIPTDIR/../../discovery
PILOTAGENTPATH=$WORKSPACE/cmd/pilot-agent
PILOTDISCOVERYPATH=$WORKSPACE/cmd/pilot-discovery
PREPAREPROXYSCRIPT=$WORKSPACE/docker

# grab ISTIO_PROXY_BUCKET from pilot/WORKSPACE
ISTIO_PROXY_BUCKET=$(sed 's/ = /=/' <<< $( awk '/ISTIO_PROXY_BUCKET =/' $WORKSPACE/WORKSPACE))
PROXYVERSION=$(sed 's/[^"]*"\([^"]*\)".*/\1/' <<<  $ISTIO_PROXY_BUCKET)
# configure whether you want debug or not
PROXY=debug-$PROXYVERSION
echo Using proxy version $PROXY

set -x
set -o errexit

# Build the pilot agent binary
cd $PILOTAGENTPATH && bazel build :pilot-agent
STATUS=$?
if [ $STATUS -ne 0 ]; then
    echo -e "\n***********\nFAILED: build failed for pilot agent.\n***********\n"
    exit $STATUS
fi

# Build the pilot discovery binary
cd $PILOTDISCOVERYPATH && bazel build :pilot-discovery
STATUS=$?
if [ $STATUS -ne 0 ]; then
    echo -e "\n***********\nFAILED: build failed for pilot discovery.\n***********\n"
    exit $STATUS
fi

cd $DISCOVERYDIR
rm -f pilot-discovery && cp $BINDIR/cmd/pilot-discovery/pilot-discovery $_
docker build -t $HUB/discovery:latest .
rm -f pilot-discovery

cd $SCRIPTDIR

# Copy the pilot agent binary to each app dir
# Build the images and  push them to hub
for app in details productpage ratings; do
  rm -f $APPSDIR/$app/pilot-agent && cp $BINDIR/cmd/pilot-agent/pilot-agent $_
  rm -f $APPSDIR/$app/prepare_proxy.sh && cp $PREPAREPROXYSCRIPT/prepare_proxy.sh $_
  docker build --build-arg PROXY=$PROXY -f $APPSDIR/$app/Dockerfile.sidecar -t "$HUB/${app}-v1:latest" $app/
  rm -f $APPSDIR/$app/pilot-agent $APPSDIR/$app/prepare_proxy.sh
done

REVIEWSDIR=$APPSDIR/reviews/reviews-wlpcfg

pushd $SCRIPTDIR/reviews
    docker run --rm -v `pwd`:/usr/bin/app:rw niaquinto/gradle clean build
popd

rm -f $REVIEWSDIR/pilot-agent && cp $BINDIR/cmd/pilot-agent/pilot-agent $REVIEWSDIR
rm -f $REVIEWSDIR/prepare_proxy.sh && cp $PREPAREPROXYSCRIPT/prepare_proxy.sh $REVIEWSDIR
#plain build -- no ratings
docker build --build-arg PROXY=$PROXY -t $HUB/reviews-v1:latest --build-arg service_version=v1 \
    -f $APPSDIR/reviews/reviews-wlpcfg/Dockerfile.sidecar reviews/reviews-wlpcfg
#with ratings black stars
docker build --build-arg PROXY=$PROXY -t $HUB/reviews-v2:latest --build-arg service_version=v2 \
    --build-arg enable_ratings=true -f $APPSDIR/reviews/reviews-wlpcfg/Dockerfile.sidecar reviews/reviews-wlpcfg
#with ratings red stars
docker build --build-arg PROXY=$PROXY -t $HUB/reviews-v3:latest --build-arg service_version=v3 \
    --build-arg enable_ratings=true --build-arg star_color=red -f $APPSDIR/reviews/reviews-wlpcfg/Dockerfile.sidecar reviews/reviews-wlpcfg
rm -f $REVIEWSDIR/pilot-agent $REVIEWSDIR/prepare_proxy.sh
