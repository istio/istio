package egress

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"

	corev1 "k8s.io/api/core/v1"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kube2 "istio.io/istio/pkg/test/kube"
)

const (
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 120 * time.Second
	// Name of secret used by egress
	secretName     = "istio-egressgateway-certs"
	istioLabel     = "istio-egressgateway"
	deploymentName = "istio-egressgateway"
)

var (
	_ Egress = &kubeEgress{}
)

type kubeEgress struct {
	id                   resource.ID
	accessor             *kube2.Accessor
	istioSystemNamespace string
}

func NewKubeComponent(ctx resource.Context, cfg Config) (Egress, error) {
	c := &kubeEgress{}
	c.id = ctx.TrackResource(c)
	env := ctx.Environment().(*kube.Environment)

	c.accessor = env.Accessor
	c.istioSystemNamespace = cfg.Istio.Settings().SystemNamespace

	err := c.accessor.WaitForFilesInPod(c.istioSystemNamespace, fmt.Sprintf("istio=%s", istioLabel), []string{"/etc/certs/cert-chain.pem"}, secretWaitTime)
	if err != nil {
		return nil, err
	}
	if cfg.Secret != nil {
		_, err := c.configureSecretAndWaitForExistence(cfg.Secret)
		if err != nil {
			return nil, err
		}
		if cfg.AdditionalSecretMountPoint != "" {
			err = c.addSecretMountPoint(cfg.AdditionalSecretMountPoint)
			if err != nil {
				return nil, err
			}
		}
	}

	return c, nil
}

func (c *kubeEgress) ID() resource.ID {
	return c.id
}

func (c *kubeEgress) configureSecretAndWaitForExistence(secret *corev1.Secret) (*corev1.Secret, error) {
	secret.Name = secretName
	secretAPI := c.accessor.GetSecret(c.istioSystemNamespace)
	_, err := secretAPI.Create(secret)
	if err != nil {
		switch t := err.(type) {
		case *errors2.StatusError:
			if t.ErrStatus.Reason == v1.StatusReasonAlreadyExists {
				_, err := secretAPI.Update(secret)
				if err != nil {
					return nil, err
				}
			}
		default:
			return nil, err
		}
	}
	secret, err = c.accessor.WaitForSecret(secretAPI, secretName, secretWaitTime)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for key := range secret.Data {
		files = append(files, "/etc/istio/egressgateway-certs/"+key)
	}
	err = c.accessor.WaitForFilesInPod(c.istioSystemNamespace, fmt.Sprintf("istio=%s", istioLabel), files, secretWaitTime)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

func (c *kubeEgress) addSecretMountPoint(path string) error {
	deployment, err := c.accessor.GetDeployment(c.istioSystemNamespace, deploymentName)
	if err != nil {
		return err
	}
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{MountPath: path, Name: "egressgateway-certs", ReadOnly: true})

	_, err = c.accessor.UpdateDeployment(deployment)
	return err
}
