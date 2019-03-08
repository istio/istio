.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o statsdadapter .
docker: statsdadapter
	docker build . -t your-repo/statsdadapter:0.1.0
docker.push: 
	docker push your-repo/statsdadapter:0.1.0
