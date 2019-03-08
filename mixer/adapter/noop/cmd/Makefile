.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o noopadapter .
docker: noopadapter
	docker build . -t your-repo/noopadapter:0.1.0
docker.push: 
	docker push your-repo/noopadapter:0.1.0
