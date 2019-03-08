.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o bypassadapter .
docker: bypassadapter
	docker build . -t your-repo/bypassadapter:0.1.0
docker.push: 
	docker push your-repo/bypassadapter:0.1.0
