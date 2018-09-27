.PHONY: deploy docker

noopadapter: main.go
	../../../../bin/gobuild.sh noopbackend istio.io/istio/pkg/version .

docker: noopbackend Dockerfile
	docker build . -t gcr.io/mixologist-142215/noopadapter:latest

deploy:
	kubectl -n istio-system apply -f noopadapter.yaml
