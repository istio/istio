.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o listcheckeradapter .
docker: listcheckeradapter
	docker build . -t your-repo/listcheckeradapter:0.1.0
docker.push: 
	docker push your-repo/listcheckeradapter:0.1.0
