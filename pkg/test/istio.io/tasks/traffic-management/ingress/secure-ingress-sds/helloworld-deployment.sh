#!/bin/bash

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: helloworld-v1
  labels:
    app: helloworld-v1
spec:
  ports:
  - name: http
    port: 5000
  selector:
    app: helloworld-v1
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: helloworld-v1
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: helloworld-v1
    spec:
      containers:
      - name: helloworld
        image: istio/examples-helloworld-v1
        resources:
          requests:
            cpu: "100m"
        imagePullPolicy: IfNotPresent #Always
        ports:
        - containerPort: 5000
EOF
