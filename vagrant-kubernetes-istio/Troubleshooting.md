This file helps note down issues we have seen and how to debug them

1. If you have problem with the `make docker` step in the [Build istio images](README.md#3-build-istio-images) step in 
   [Readme](README.md), please try to clean up all the built binaries and run the test setup script again.
   ```sh
   cd $ISTIO/istio
   GOOS=linux make clean
   ```
   The `GOOS=linux` is required when you are running setup on macOS.
1. When installing docker if you see errors about missing packages on linux, please download and then retry docker installation.
1. If your prereqs installation, seems to be stuck, try restarting the box..
1. When running tests, if you get "Bad Request" error
   Vagrant VM uses insecure localregistry opened at 10.0.0.2:5000, make sure HUB and TAG are as follows:
   
   ```bash
   echo $HUB
   // It should return "10.10.0.2:5000"
   echo $TAG
   // It should return "latest"
   ```
1. If your machine complains of low disk space, try clean up docker images from it.
   To cleanup all docker images on your machine,run following command:
   
   ```bash
   docker images -q |xargs docker rmi
   ```
   
1. setup_vm.sh chooses port 5000 to push docker images to VM, and 8080 to forward kubectl requests to VM. If any of these ports is already used by other applications, you can change the port.
   * Open Vagrantfile, find these two lines and change host port to the one you prefer. E.g. change `host: 5000` to `host: 5001`
  
   ```bash
   config.vm.network "forwarded_port", guest: 5000, host: 5000, host_ip: "10.10.0.2"
   config.vm.network "forwarded_port", guest: 8080, host: 8080
   ```
   After this change you can run `sh setup_vm.sh`.

   * If you want to change port 5000 for pushing docker images to VM, then you also need to modify setup_dockerdaemon_linux.sh. Find the line below and change 10.10.0.2:5000 to 10.10.0.2:5001
   ```bash
   sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service
   ```

   After this change you can run `sh setup_host.sh`.
   
   * If you want to change port 8080 for sending kubectl requests to VM, then after running `sh setup_host.sh`, you need to open file `~/.kube/config` in your host machine, find this line and change the port 8080 to any port you prefer.
   ```bash
   server: http://localhost:8080
   ```
