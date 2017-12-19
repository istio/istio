.PHONY: docker

default: docker

clean:
	rm -rf docker/viz
	rm -f docker/servicegraph

docker: docker/viz docker/servicegraph 
	cd docker && docker build -t servicegraph -f Dockerfile .
	cd docker && docker build -t servicegraph_debug -f Dockerfile.debug .

docker/viz:
	cp -a js/viz docker

docker/servicegraph:
	../../../bin/gobuild.sh docker/servicegraph istio.io/istio/mixer/pkg/version ./cmd/server
