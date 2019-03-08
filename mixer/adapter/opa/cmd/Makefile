.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o opaadapter .
docker: opaadapter
	docker build . -t your-repo/opaadapter:0.1.0
docker.push: 
	docker push your-repo/opaadapter:0.1.0
