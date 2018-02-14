If you want to make a local change and test some component, say istio-ca, you could follow below steps:

Under istio/istio repo
$ pwd .../istio.io/istio

Set up environment variables HUB and TAG by
$ export HUB=gcr.io/testing
$ export TAG=istio-ca

Make some local change of CA code, then build istio-ca
$ make docker.istio-ca

Push docker image
$ make push.docker.istio-ca
