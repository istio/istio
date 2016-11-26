# Building the Mixer

The mixer build environment is pretty simple. Getting the source code and typing 'make'
in the root will get you a working mixer binary.

To learn more...

- [Installing Prerequisites](#installing-prerequisites)
- [Getting Stuff Done](#getting-stuff-done)

## Installing Prerequisites

1. Install go

2. Install make
```
    sudo apt-get install build-essential
```

3. Install Docker
```
    sudo apt-get docker-engine
```

4. Install gcloud
```
    curl https://sdk.cloud.google.com | bash
    exec -l $SHELL
    gcloud init
```

5. Install kubectl
```
    gcloud components install kubectl
```

6. Install python-pip
```
    sudo apt-get install python-pip
```

## Getting Stuff Done

1. Building. The first run of make downloads dependencies using ``glide``
```
    make
```

2. Run the tests
```
    make test
```

3. Delete any produced outputs from a build
```
    make clean
```
