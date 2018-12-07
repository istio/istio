# HTTPS service

This sample consists of an NGINX service which can encrypt traffic using HTTPS.

To use it:

1. Generate certificates

    You need to have openssl installed to run these commands:

    ```console
    $ openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/nginx.key -out /tmp/nginx.crt -subj "/CN=my-nginx/O=my-nginx"
    ```

    ```console
    $ kubectl create secret tls nginxsecret --key /tmp/nginx.key --cert /tmp/nginx.crt
    ```

2. Create a configmap used for the HTTPS service

    ```console
    $ kubectl create configmap nginxconfigmap --from-file=samples/https/default.conf
    ```

3. Deploy a NGINX-based HTTPS service

    `kubectl apply -f samples/https/nginx-app.yaml`

Call my-nginx service `curl https://my-nginx -k` to verify this creation.
