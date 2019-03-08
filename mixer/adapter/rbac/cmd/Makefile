.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o rbacadapter .
docker: rbacadapter
	docker build . -t your-repo/rbacadapter:0.1.0
docker.push: 
	docker push your-repo/rbacadapter:0.1.0
