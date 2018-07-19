#! /bin/bash
set -x
# Check we can build the image
make docker-internal TAG=webtest || exit 1
FORTIO_UI_PREFIX=/newprefix/ # test the non default prefix (not /fortio/)
FILE_LIMIT=20 # must be low to detect leaks
LOGLEVEL=info # change to debug to debug
MAXPAYLOAD=8 # Max Payload size for echo?size= in kb
CERT=/etc/ssl/certs/ca-certificates.crt
TEST_CERT_VOL=/etc/ssl/certs/fortio
DOCKERNAME=fortio_server
DOCKERSECNAME=fortio_secure_server
DOCKERSECVOLNAME=fortio_certs
DOCKERID=$(docker run -d --ulimit nofile=$FILE_LIMIT --name $DOCKERNAME istio/fortio:webtest server -ui-path $FORTIO_UI_PREFIX -loglevel $LOGLEVEL -maxpayloadsizekb $MAXPAYLOAD)
function cleanup {
  set +e # errors are ok during cleanup
  docker stop $DOCKERID
  docker rm -f $DOCKERNAME
  docker stop $DOCKERSECID # may not be set yet, it's ok
  docker rm -f $DOCKERSECNAME
  docker rm -f $DOCKERSECVOLNAME
}
trap cleanup EXIT
set -e
set -o pipefail
docker ps
BASE_URL="http://localhost:8080"
BASE_FORTIO="$BASE_URL$FORTIO_UI_PREFIX"
CURL="docker exec $DOCKERNAME /usr/local/bin/fortio curl -loglevel $LOGLEVEL"
# Check https works (certs are in the image) - also tests autoswitch to std client for https
$CURL https://istio.io/robots.txt
# Check that browse doesn't 404s
$CURL ${BASE_FORTIO}browse
# Check we can connect, and run a http QPS test against ourselves through fetch
$CURL "${BASE_FORTIO}fetch/localhost:8080$FORTIO_UI_PREFIX?url=http://localhost:8080/debug&load=Start&qps=-1&json=on" | grep ActualQPS
# Check we can do it twice despite ulimit - check we get all 200s (exactly 80 of them (default is 8 connections->16 fds + a few))
$CURL "${BASE_FORTIO}fetch/localhost:8080$FORTIO_UI_PREFIX?url=http://localhost:8080/debug&load=Start&n=80&qps=-1&json=on" | grep '"200": 80'
# Check we can connect, and run a grpc QPS test against ourselves through fetch
$CURL "${BASE_FORTIO}fetch/localhost:8080$FORTIO_UI_PREFIX?url=localhost:8079&load=Start&qps=-1&json=on&n=100&runner=grpc" | grep '"1": 100'
# Check we get the logo (need to remove the CR from raw headers)
VERSION=$(docker exec $DOCKERNAME /usr/local/bin/fortio version -s)
LOGO_TYPE=$($CURL "${BASE_FORTIO}${VERSION}/static/img/logo.svg" | grep -i Content-Type: | tr -d '\r'| awk '{print $2}')
if [ "$LOGO_TYPE" != "image/svg+xml" ]; then
  echo "Unexpected content type for the logo: $LOGO_TYPE"
  exit 1
fi
# Check we can get the JS file through the proxy and it's > 50k
SIZE=$($CURL "${BASE_FORTIO}fetch/localhost:8080${FORTIO_UI_PREFIX}${VERSION}/static/js/Chart.min.js" |wc -c)
if [ "$SIZE" -lt 50000 ]; then
  echo "Too small fetch for js: $SIZE"
  exit 1
fi
# Check if max payload set to value passed in cmd line parameter -maxpayloadsizekb
SIZE=$($CURL "${BASE_URL}/echo?size=1048576" |wc -c)
# Payload is 8192 but between content chunking and headers fast client can return up to 8300 or so
if [ "$SIZE" -lt 8191 ] || [ "$SIZE" -gt 8400 ]; then
  echo "-maxpayloadsizekb not working as expected"
  exit 1
fi

# Check the main page
$CURL $BASE_FORTIO
# Do a small http load using std client
docker exec $DOCKERNAME /usr/local/bin/fortio load -stdclient -qps 1 -t 2s -c 1 https://www.google.com/
# and with normal and with custom headers
docker exec $DOCKERNAME /usr/local/bin/fortio load -H Foo:Bar -H Blah:Blah -qps 1 -t 2s -c 2 http://www.google.com/
# Do a grpcping
docker exec $DOCKERNAME /usr/local/bin/fortio grpcping localhost
# Do a grpcping to a scheme-prefixed destination. Fortio should append port number
docker exec $DOCKERNAME /usr/local/bin/fortio grpcping https://fortio.istio.io
docker exec $DOCKERNAME /usr/local/bin/fortio grpcping http://fortio.istio.io
# Do a grpcping with -cert flag. Fortio should use valid cert.
docker exec $DOCKERNAME /usr/local/bin/fortio grpcping -cacert $CERT fortio.istio.io:443
docker exec $DOCKERNAME /usr/local/bin/fortio grpcping -cacert $CERT https://fortio.istio.io
# Do a local grpcping. Fortio should append default grpc port number to destination
docker exec $DOCKERNAME /usr/local/bin/fortio grpcping localhost
# pprof should be there, no 404/error
PPROF_URL="$BASE_URL/debug/pprof/heap?debug=1"
$CURL $PPROF_URL | grep -i TotalAlloc # should find this in memory profile
# creating dummy container to hold a volume for test certs due to remote docker bind mount limitation.
DOCKERVOLID=$(docker create -v $TEST_CERT_VOL --name $DOCKERSECVOLNAME istio/fortio.build:v8 /bin/true)
# copying cert files into the certs volume of the dummy container
for f in ca.crt server.crt server.key; do docker cp $PWD/cert-tmp/$f $DOCKERSECVOLNAME:$TEST_CERT_VOL/$f; done
# start server in secure grpc mode. uses non-default ports to avoid conflicts with fortio_server container.
# mounts certs volume from dummy container.
DOCKERSECID=$(docker run -d --ulimit nofile=$FILE_LIMIT --name $DOCKERSECNAME --volumes-from $DOCKERSECVOLNAME istio/fortio:webtest server -cacert $TEST_CERT_VOL/ca.crt -cert $TEST_CERT_VOL/server.crt -key $TEST_CERT_VOL/server.key -grpc-port 8097 -http-port 8098 -redirect-port 8090 -loglevel $LOGLEVEL)
# run secure grpcping and load tests
docker exec $DOCKERSECNAME /usr/local/bin/fortio grpcping -cacert $TEST_CERT_VOL/ca.crt localhost:8097
docker exec $DOCKERSECNAME /usr/local/bin/fortio load -grpc -cacert $TEST_CERT_VOL/ca.crt localhost:8097
# switch to report mode
docker stop $DOCKERID
docker rm $DOCKERNAME
DOCKERNAME=fortio_report
DOCKERID=$(docker run -d --ulimit nofile=$FILE_LIMIT --name $DOCKERNAME istio/fortio:webtest report -loglevel $LOGLEVEL)
docker ps
CURL="docker exec $DOCKERNAME /usr/local/bin/fortio curl -loglevel $LOGLEVEL"
if $CURL $PPROF_URL ; then
  echo "pprof should 404 on report mode!"
  exit 1
else
  echo "expected pprof failure to access in report mode - good !"
fi
# base url should serve report only UI in report mode
$CURL $BASE_URL | grep "report only limited UI"
# cleanup() will clean everything left even on success
