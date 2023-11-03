package config

import (
	"context"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

type KubeClient struct {
	*kubernetes.Clientset
}


// NewKubeClient returns a KubeClient for the given config
//
// Args:
//   - api-server URL as string (optional)
//   - kubeconfigPath as string
//
// Returns:
//   - KubeClient or error
func NewKubeClient(apiserverURL, kubeconfigPath string) (*KubeClient, error) {
	config, err := utils.GetClientConfig(apiserverURL, kubeconfigPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err":    err,
			"path":   kubeconfigPath,
			"apiurl": apiserverURL,
		}).Error("Failed to get client config")
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.WithFields(logrus.Fields{
			"err": err,
		}).Error("Failed to get client")
		return nil, err
	}

	return &KubeClient{
		Clientset: kubeClient,
	}, err
}

// ListNodes will return a list of all nodes in the cluster
//
// Args:
//   - labelSelector as string (optional)
//
// Returns:
//   - v1.NodeList or error
func (c *KubeClient) ListNodes(labelSelector string) (*v1.NodeList, error) {
	return c.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}
