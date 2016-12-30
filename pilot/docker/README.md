Obtain docker daemon environment variables by running:

    eval $(minikube docker-env)

You can use bazel to build docker images for the agent, and push it to minikube:

    bazel run //docker:kube_agent-docker
    kubectl run --image bazel/docker:kube_agent-docker example
