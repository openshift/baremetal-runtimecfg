package nodeconfig

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const (
	defaultWatchRetryInterval = 5 * time.Second
)

// NodeWatcher watches the Kubernetes Node objects and maintains a cached list
// of nodes to avoid repeated API calls. It automatically updates the cache
// when nodes are added, modified, or deleted.
type NodeWatcher struct {
	client kubernetes.Interface

	nodeList      *v1.NodeList
	nodeListMutex sync.RWMutex

	watchRetryInterval time.Duration
}

// NewNodeWatcher creates a new NodeWatcher using the given kubeconfig.
func NewNodeWatcher(kubeconfigPath string) (*NodeWatcher, error) {
	clientConfig, err := utils.GetClientConfig("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	w := &NodeWatcher{
		client:             client,
		nodeList:           &v1.NodeList{},
		watchRetryInterval: defaultWatchRetryInterval,
	}

	return w, nil
}

// Run keeps watching the Nodes until the context is cancelled.
// Any errors are logged and watching is re-established.
func (w *NodeWatcher) Run(ctx context.Context) {
	// Keep watching until the context is cancelled.
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := w.watchNodes(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}

			log.WithError(err).Warn("Node watcher failed")
		}
	}, w.watchRetryInterval)
}

func (w *NodeWatcher) watchNodes(ctx context.Context) error {
	watchOpts := metav1.ListOptions{
		ResourceVersionMatch: metav1.ResourceVersionMatchNotOlderThan,
	}
	watchOpts.SendInitialEvents = ptr.To(true)

	watcher, err := w.client.CoreV1().Nodes().Watch(ctx, watchOpts)
	if err != nil {
		return err
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}

			switch event.Type {
			case watch.Added:
				node := event.Object.(*v1.Node)
				w.addOrUpdateNode(node)
				log.WithFields(logrus.Fields{
					"nodeName": node.Name,
				}).Debug("Node added to cache")

			case watch.Modified:
				node := event.Object.(*v1.Node)
				w.addOrUpdateNode(node)
				log.WithFields(logrus.Fields{
					"nodeName": node.Name,
				}).Debug("Node updated in cache")

			case watch.Deleted:
				node := event.Object.(*v1.Node)
				w.deleteNode(node)
				log.WithFields(logrus.Fields{
					"nodeName": node.Name,
				}).Debug("Node removed from cache")

			case watch.Bookmark:
				// Bookmark events are used for tracking the resource version
				// No action needed

			case watch.Error:
				log.WithFields(logrus.Fields{
					"error": event.Object,
				}).Warn("Watch error event received")
				return nil
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (w *NodeWatcher) addOrUpdateNode(node *v1.Node) {
	w.nodeListMutex.Lock()
	defer w.nodeListMutex.Unlock()

	// Check if node already exists
	for i, existingNode := range w.nodeList.Items {
		if existingNode.Name == node.Name {
			// Update existing node
			w.nodeList.Items[i] = *node
			return
		}
	}

	// Add new node
	w.nodeList.Items = append(w.nodeList.Items, *node)
}

func (w *NodeWatcher) deleteNode(node *v1.Node) {
	w.nodeListMutex.Lock()
	defer w.nodeListMutex.Unlock()

	// Find and remove the node
	for i, existingNode := range w.nodeList.Items {
		if existingNode.Name == node.Name {
			w.nodeList.Items = append(w.nodeList.Items[:i], w.nodeList.Items[i+1:]...)
			return
		}
	}
}

// GetNodes returns a deep copy of the cached node list.
func (w *NodeWatcher) GetNodes() *v1.NodeList {
	w.nodeListMutex.RLock()
	defer w.nodeListMutex.RUnlock()
	return w.nodeList.DeepCopy()
}

// GetMasterNodes returns a deep copy of the cached node list filtered to only master nodes.
func (w *NodeWatcher) GetMasterNodes() *v1.NodeList {
	w.nodeListMutex.RLock()
	defer w.nodeListMutex.RUnlock()

	masterNodes := &v1.NodeList{}
	for _, node := range w.nodeList.Items {
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			masterNodes.Items = append(masterNodes.Items, node)
		}
	}

	return masterNodes
}

// GetNodeCount returns the current number of nodes in the cache.
func (w *NodeWatcher) GetNodeCount() int {
	w.nodeListMutex.RLock()
	defer w.nodeListMutex.RUnlock()
	return len(w.nodeList.Items)
}
