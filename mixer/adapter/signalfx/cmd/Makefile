.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o signalfxadapter .
docker: signalfxadapter
	docker build . -t your-repo/signalfxadapter:0.1.0
docker.push: 
	docker push your-repo/signalfxadapter:0.1.0
