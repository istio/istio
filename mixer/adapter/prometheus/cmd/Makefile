.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o prometheusadapter .
docker: prometheusadapter
	docker build . -t your-repo/prometheusadapter:0.1.0
docker.push: 
	docker push your-repo/prometheusadapter:0.1.0
