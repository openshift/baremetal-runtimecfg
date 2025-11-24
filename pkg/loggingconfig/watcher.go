package loggingconfig

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
	"github.com/openshift/baremetal-runtimecfg/pkg/utils"
)

const (
	defaultWatchRetryInterval = 5 * time.Second

	// ConfigMapDebugKey is the ConfigMap key that is used when looking for debug logging configuration.
	ConfigMapDebugKey = "enable-nodeip-debug"
)

// Watcher watches a ConfigMap for changes to ConfigMapDebugKey and
// dynamically adjusts the logging level for the config and utils packages in the following way:
//   - When the ConfigMap is created and ConfigMapDebugKey is present and can be parsed as a boolean,
//     debug is set to the associated value. Otherwise, debug mode is disabled.
//   - When the ConfigMap is modified, the same rules as for creation apply.
//   - When the ConfigMap is deleted, debug mode is disabled.
//
// When IS_BOOTSTRAP environment variable is set to "yes", the logging level is always debug.
type Watcher struct {
	client kubernetes.Interface

	cmNamespace string
	cmName      string

	updateLogging      func(debug bool, err error)
	watchRetryInterval time.Duration

	debugEnabled bool
}

// NewWatcher creates a new Watcher using the given kubeconfig for watching the specified ConfigMap.
func NewWatcher(kubeconfigPath string, cmNamespace string, cmName string) (*Watcher, error) {
	clientConfig, err := utils.GetClientConfig("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		client:             client,
		cmNamespace:        cmNamespace,
		cmName:             cmName,
		watchRetryInterval: defaultWatchRetryInterval,
	}
	w.updateLogging = func(debug bool, err error) {
		if err == nil {
			w.setDebugEnabled(debug, false)
		}
	}

	// Set up initial logging state based on environment variables.
	w.setDebugEnabled(false, true)
	return w, nil
}

// Run keeps watching the ConfigMap until the context is cancelled.
// Any errors are logged and watching is re-established.
func (w *Watcher) Run(ctx context.Context) {
	// Keep watching until the context is cancelled.
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := w.watchConfigMap(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}

			log.WithError(err).WithFields(logrus.Fields{
				"configMapNamespace": w.cmNamespace,
				"configMapName":      w.cmName,
			}).Warn("Logging configuration watcher failed")
		}
	}, w.watchRetryInterval)
}

func (w *Watcher) watchConfigMap(ctx context.Context) error {
	watchOpts := metav1.SingleObject(metav1.ObjectMeta{Name: w.cmName})
	watchOpts.ResourceVersionMatch = metav1.ResourceVersionMatchNotOlderThan
	watchOpts.SendInitialEvents = ptr.To(true)

	watcher, err := w.client.CoreV1().ConfigMaps(w.cmNamespace).Watch(ctx, watchOpts)
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
			case watch.Added, watch.Modified:
				value, ok := event.Object.(*corev1.ConfigMap).Data[ConfigMapDebugKey]
				if !ok {
					w.updateLogging(false, nil)
				} else {
					debug, _ := strconv.ParseBool(value)
					w.updateLogging(debug, nil)
				}

			case watch.Deleted:
				w.updateLogging(false, nil)

			case watch.Error:
				err := fmt.Errorf("watch error event received: %v", event.Object)
				w.updateLogging(false, err)
				return err
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (w *Watcher) setDebugEnabled(debug bool, forceUpdate bool) {
	if os.Getenv("IS_BOOTSTRAP") == "yes" {
		debug = true
	}

	if w.debugEnabled == debug && !forceUpdate {
		return
	}

	if debug {
		log.Info("Debug logging enabled")
		config.SetDebugLogLevel()
		utils.SetDebugLogLevel()
	} else {
		log.Info("Debug logging disabled")
		config.SetInfoLogLevel()
		utils.SetInfoLogLevel()
	}

	w.debugEnabled = debug
}
