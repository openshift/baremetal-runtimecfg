package config

import (
	"sync"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	v1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

type KubeClient struct {
	*kubernetes.Clientset
	sync.Mutex

	factory      informers.SharedInformerFactory
	nodeInformer v1informers.NodeInformer
	stopCh       <-chan struct{}
}

// NewKubeClient returns a KubeClient for the given config
//
// Args:
//   - api-server URL as string (optional)
//   - kubeconfigPath as string
//   - stopCh as a stop channel
//
// Returns:
//   - KubeClient or error
func NewKubeClient(apiserverURL, kubeconfigPath string, stopCh <-chan struct{}) (*KubeClient, error) {
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
		stopCh:    stopCh,
	}, err
}

// Start starts a KubeClient
func (c *KubeClient) Start() {
	c.Lock()
	if c.factory == nil {
		c.factory = informers.NewSharedInformerFactory(c, 0)
		c.nodeInformer = c.factory.Core().V1().Nodes()
	}
	c.Unlock()

	c.factory.Start(c.stopCh)
	if ok := cache.WaitForCacheSync(c.stopCh, c.nodeInformer.Informer().HasSynced); !ok {
		log.Warn("Failed to wait for node cache to sync")
	}
}

// ListNodes will return a list of all nodes in the cluster
//
// Args:
//   - labelSelector as string (optional)
//
// Returns:
//   - []*v1.Node or error
func (c *KubeClient) ListNodes(labelSelector string) ([]*v1.Node, error) {
	// Defer client start until it's actually needed
	c.Start()

	selector := labels.Everything()
	if labelSelector != "" {
		ls, err := metav1.ParseToLabelSelector(labelSelector)
		if err != nil {
			return nil, err
		}
		selector, err = metav1.LabelSelectorAsSelector(ls)
		if err != nil {
			return nil, err
		}
	}
	return c.nodeInformer.Lister().List(selector)
}
