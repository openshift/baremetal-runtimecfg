package loggingconfig

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stest "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

const (
	testNamespace     = "test-namespace"
	testConfigMapName = "logging"

	interval = 10 * time.Millisecond
	timeout  = 3 * time.Second
)

func newConfigMap(debugEnabled *bool) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{},
	}
	if debugEnabled != nil {
		if *debugEnabled {
			cm.Data[ConfigMapDebugKey] = "true"
		} else {
			cm.Data[ConfigMapDebugKey] = "false"
		}
	}
	return cm
}

type configUpdateEvent struct {
	Debug bool
	Err   error
}

var _ = Describe("Watcher", func() {
	Describe("Run", func() {
		// Set up the whole thing so that we can add events to a fake watcher and receive config changes on a channel.
		// All test cases are then very similar. Add events to the fake watcher and check changes on the config channel.
		var (
			ctx         context.Context
			cancel      context.CancelFunc
			fakeClient  *fake.Clientset
			fakeWatcher *watch.FakeWatcher
			eventCh     chan configUpdateEvent
			watcher     *Watcher
			wg          sync.WaitGroup
		)

		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())

			fakeClient = fake.NewSimpleClientset()
			fakeWatcher = watch.NewFake()
			fakeClient.PrependWatchReactor("configmaps", k8stest.DefaultWatchReactor(fakeWatcher, nil))

			eventCh = make(chan configUpdateEvent, 10)

			watcher = &Watcher{
				client:      fakeClient,
				cmNamespace: testNamespace,
				cmName:      testConfigMapName,
				updateLogging: func(debug bool, err error) {
					eventCh <- configUpdateEvent{Debug: debug, Err: err}
				},
				watchRetryInterval: 5 * time.Second,
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				watcher.Run(ctx)
			}()

			// Make sure the watcher is running by adding ConfigMap events until config event changes appear.
			Eventually(func() bool {
				fakeWatcher.Add(&corev1.ConfigMap{})

				select {
				case <-eventCh:
					return true
				default:
					return false
				}
			}, interval, timeout).Should(BeTrue())

			// Make sure eventCh is drained before starting actual testing.
		DrainLoop:
			for {
				select {
				case <-eventCh:
				default:
					break DrainLoop
				}
			}
		})

		AfterEach(func() {
			cancel()
			wg.Wait()
			fakeWatcher.Stop()
		})

		Context("when ConfigMap is created with debug enabled", func() {
			It("should enable debug logging", func() {
				fakeWatcher.Add(newConfigMap(ptr.To(true)))

				var event configUpdateEvent
				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeTrue())
				Expect(event.Err).To(BeNil())
			})
		})

		Context("when ConfigMap is created with debug disabled", func() {
			It("should disable debug logging", func() {
				fakeWatcher.Add(newConfigMap(ptr.To(false)))

				var event configUpdateEvent
				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeFalse())
				Expect(event.Err).To(BeNil())
			})
		})

		Context("when ConfigMap is created without relevant data value", func() {
			It("should disable debug logging", func() {
				fakeWatcher.Add(newConfigMap(nil))

				var event configUpdateEvent
				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeFalse())
				Expect(event.Err).To(BeNil())
			})
		})

		Context("when ConfigMap is modified", func() {
			It("should switch debug logging accordingly", func() {
				cm := newConfigMap(ptr.To(false))
				fakeWatcher.Add(cm.DeepCopy())

				var event configUpdateEvent
				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeFalse())
				Expect(event.Err).To(BeNil())

				// Toggle multiple times.
				for i := 0; i < 5; i++ {
					var expectedDebug bool
					if i%2 == 0 {
						cm.Data[ConfigMapDebugKey] = "true"
						expectedDebug = true
					} else {
						cm.Data[ConfigMapDebugKey] = "false"
						expectedDebug = false
					}

					fakeWatcher.Modify(cm.DeepCopy())

					Eventually(eventCh, timeout).Should(Receive(&event))
					Expect(event.Debug).To(Equal(expectedDebug))
					Expect(event.Err).To(BeNil())
				}
			})
		})

		Context("when ConfigMap is deleted", func() {
			It("should disable debug logging", func() {
				cm := newConfigMap(ptr.To(true))
				fakeWatcher.Add(cm.DeepCopy())

				var event configUpdateEvent
				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeTrue())
				Expect(event.Err).To(BeNil())

				fakeWatcher.Delete(cm.DeepCopy())

				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeFalse())
				Expect(event.Err).To(BeNil())
			})
		})

		Context("when watch error event is received", func() {
			It("should invoke the callback with the relevant error", func() {
				fakeWatcher.Action(watch.Error, nil)

				var event configUpdateEvent
				Eventually(eventCh, timeout).Should(Receive(&event))
				Expect(event.Debug).To(BeFalse())
				Expect(event.Err).To(Not(BeNil()))
			})
		})
	})
})
