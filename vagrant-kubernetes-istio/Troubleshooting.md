This file helps note down issues we have seen and how to debug them

1) When installing docker if you see errors about missing packages on linux, please download and then retry docker installation.
2) If your prereqs installation, seems to be stuck, try restarting the box..
3) When running tests, if you get "Bad Request" error
   Vagrant VM uses insecure localregistry opened at 10.0.0.2:5000, make sure HUB and TAG are as follows:
   
   ```bash
   echo $HUB
   // It should return "10.10.0.2:5000"
   echo $TAG
   // It should return "latest"
   ```
4) If your machine complains of low disk space, try clean up docker images from it.
   To cleanup all docker images on your machine,run following command:
   
   ```bash
   docker images -q |xargs docker rmi
   ```
