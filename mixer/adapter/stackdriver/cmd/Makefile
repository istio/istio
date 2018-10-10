.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o stackdriveradapter .
docker: stackdriveradapter
	docker build . -t your-repo/stackdriveradapter:0.1.0
docker.push: 
	docker push your-repo/stackdriveradapter:0.1.0
