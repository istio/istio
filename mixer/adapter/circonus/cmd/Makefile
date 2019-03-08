.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o circonusadapter .
docker: circonusadapter
	docker build . -t your-repo/circonusadapter:0.1.0
docker.push: 
	docker push your-repo/circonusadapter:0.1.0
