.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o denieradapter .
docker: denieradapter
	docker build . -t your-repo/denieradapter:0.1.0
docker.push: 
	docker push your-repo/denieradapter:0.1.0
