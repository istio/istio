.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o zipkinadapter .
docker: zipkinadapter
	docker build . -t your-repo/zipkinadapter:0.1.0
docker.push: 
	docker push your-repo/zipkinadapter:0.1.0
