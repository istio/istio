.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o stdioadapter .
docker: stdioadapter
	docker build . -t your-repo/stdioadapter:0.1.0
docker.push: 
	docker push your-repo/stdioadapter:0.1.0
