# Kpt package for e2e test

This is a simplified kpt package for ASM installation. It is created based on the production kpt packages in [GitHub repo anthos-service-mesh-packages](https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages).

The purpose of this package is to workaround some limitations of istioctl --set flag (e.g. see [GitHub issue](https://github.com/istio/istio/issues/19392)). The configurations not included in this kpt package needs to be set using istioctl --set flag.

