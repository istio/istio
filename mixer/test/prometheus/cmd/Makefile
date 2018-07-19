.PHONY: deploy docker

prometheusadapter: main.go
	../../../../bin/gobuild.sh prometheusadapter istio.io/istio/pkg/version .
docker: prometheusadapter
	docker build . -t gcr.io/mixologist-142215/prometheusadapter:latest

deploy:
	kubectl -n istio-system apply -f prometheusadapter.yaml
