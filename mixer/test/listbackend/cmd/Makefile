.PHONY: deploy docker

listadapter: main.go
	../../../../bin/gobuild.sh listadapter istio.io/istio/pkg/version .
docker: listadapter Dockerfile
	docker build . -t gcr.io/mixologist-142215/listadapter:latest

deploy:
	kubectl -n istio-system apply -f listadapter.yaml
