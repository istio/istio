# Deploying an Instance

The mixer is currently only available in Kubernetes. Once you have a
usable Kubernetes cluster, you can easily deploy an instance of the
Istio mixer and start invoking its APIs.

To learn more...

- [Preparing to Run the Mixer](#preparing-to-run-the-mixer)

## Preparing to Run the Mixer

1. Run the Docker daemon
```
    sudo service docker start
```

2. Create a project to run the mixer on the [Google Cloud Console](https://pantheon.corp.google.com), and activate the Google Container
Engine API for the project.

3. Set the PROJECT_ID variable to your project's id
```
    export PROJECT_ID=<your project id>
```

4. Set the NAMESPACE variable to ${USER}-dev
```
    export NAMESPACE=${USER}-dev
```
