package nodeconfig

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("NodeWatcher", func() {
	var (
		watcher       *NodeWatcher
		fakeClientset *fake.Clientset
		fakeWatcher   *watch.FakeWatcher
		ctx           context.Context
		cancel        context.CancelFunc
	)

	BeforeEach(func() {
		fakeClientset = fake.NewSimpleClientset()
		fakeWatcher = watch.NewFake()

		// Intercept watch requests and return our fake watcher
		fakeClientset.PrependWatchReactor("nodes", func(action k8stesting.Action) (handled bool, ret watch.Interface, err error) {
			return true, fakeWatcher, nil
		})

		watcher = &NodeWatcher{
			client:             fakeClientset,
			nodeList:           &v1.NodeList{},
			watchRetryInterval: 100 * time.Millisecond,
		}

		ctx, cancel = context.WithCancel(context.Background())
	})

	AfterEach(func() {
		cancel()
		if fakeWatcher != nil {
			fakeWatcher.Stop()
		}
	})

	Describe("GetNodes", func() {
		Context("when the cache is empty", func() {
			It("should return an empty node list", func() {
				nodes := watcher.GetNodes()
				Expect(nodes).NotTo(BeNil())
				Expect(nodes.Items).To(HaveLen(0))
			})
		})

		Context("when the cache has nodes", func() {
			BeforeEach(func() {
				watcher.nodeList.Items = []v1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				}
			})

			It("should return all nodes", func() {
				nodes := watcher.GetNodes()
				Expect(nodes.Items).To(HaveLen(2))
				Expect(nodes.Items[0].Name).To(Equal("node1"))
				Expect(nodes.Items[1].Name).To(Equal("node2"))
			})

			It("should return a deep copy", func() {
				nodes := watcher.GetNodes()
				nodes.Items[0].Name = "modified"
				Expect(watcher.nodeList.Items[0].Name).To(Equal("node1"))
			})
		})
	})

	Describe("GetMasterNodes", func() {
		BeforeEach(func() {
			watcher.nodeList.Items = []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "master1",
						Labels: map[string]string{
							"node-role.kubernetes.io/master": "",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker1",
						Labels: map[string]string{
							"node-role.kubernetes.io/worker": "",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "master2",
						Labels: map[string]string{
							"node-role.kubernetes.io/master": "",
						},
					},
				},
			}
		})

		It("should return only master nodes", func() {
			masters := watcher.GetMasterNodes()
			Expect(masters.Items).To(HaveLen(2))
			Expect(masters.Items[0].Name).To(Equal("master1"))
			Expect(masters.Items[1].Name).To(Equal("master2"))
		})

		It("should not return worker nodes", func() {
			masters := watcher.GetMasterNodes()
			for _, node := range masters.Items {
				Expect(node.Name).NotTo(Equal("worker1"))
			}
		})
	})

	Describe("GetNodeCount", func() {
		Context("when cache is empty", func() {
			It("should return 0", func() {
				Expect(watcher.GetNodeCount()).To(Equal(0))
			})
		})

		Context("when cache has nodes", func() {
			BeforeEach(func() {
				watcher.nodeList.Items = []v1.Node{
					{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				}
			})

			It("should return the correct count", func() {
				Expect(watcher.GetNodeCount()).To(Equal(3))
			})
		})
	})

	Describe("addOrUpdateNode", func() {
		It("should add a new node", func() {
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "newnode"}}
			watcher.addOrUpdateNode(node)

			Expect(watcher.nodeList.Items).To(HaveLen(1))
			Expect(watcher.nodeList.Items[0].Name).To(Equal("newnode"))
		})

		It("should update an existing node", func() {
			watcher.nodeList.Items = []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status:     v1.NodeStatus{Phase: v1.NodePending},
				},
			}

			updatedNode := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status:     v1.NodeStatus{Phase: v1.NodeRunning},
			}
			watcher.addOrUpdateNode(updatedNode)

			Expect(watcher.nodeList.Items).To(HaveLen(1))
			Expect(watcher.nodeList.Items[0].Status.Phase).To(Equal(v1.NodeRunning))
		})

		It("should maintain order when updating", func() {
			watcher.nodeList.Items = []v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			}

			updatedNode := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
					Labels: map[string]string{
						"updated": "true",
					},
				},
			}
			watcher.addOrUpdateNode(updatedNode)

			Expect(watcher.nodeList.Items).To(HaveLen(3))
			Expect(watcher.nodeList.Items[1].Name).To(Equal("node2"))
			Expect(watcher.nodeList.Items[1].Labels["updated"]).To(Equal("true"))
		})
	})

	Describe("deleteNode", func() {
		BeforeEach(func() {
			watcher.nodeList.Items = []v1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			}
		})

		It("should delete an existing node", func() {
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}
			watcher.deleteNode(node)

			Expect(watcher.nodeList.Items).To(HaveLen(2))
			Expect(watcher.nodeList.Items[0].Name).To(Equal("node1"))
			Expect(watcher.nodeList.Items[1].Name).To(Equal("node3"))
		})

		It("should handle deleting non-existent node gracefully", func() {
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "nonexistent"}}
			watcher.deleteNode(node)

			Expect(watcher.nodeList.Items).To(HaveLen(3))
		})

		It("should delete the first node", func() {
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
			watcher.deleteNode(node)

			Expect(watcher.nodeList.Items).To(HaveLen(2))
			Expect(watcher.nodeList.Items[0].Name).To(Equal("node2"))
		})

		It("should delete the last node", func() {
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}
			watcher.deleteNode(node)

			Expect(watcher.nodeList.Items).To(HaveLen(2))
			Expect(watcher.nodeList.Items[1].Name).To(Equal("node2"))
		})
	})

	Describe("watchNodes integration", func() {
		It("should handle Added events", func() {
			go watcher.watchNodes(ctx)

			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
			fakeWatcher.Add(node)

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(1))

			nodes := watcher.GetNodes()
			Expect(nodes.Items[0].Name).To(Equal("node1"))
		})

		It("should handle Modified events", func() {
			go watcher.watchNodes(ctx)

			// Add initial node
			node := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status:     v1.NodeStatus{Phase: v1.NodePending},
			}
			fakeWatcher.Add(node)

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(1))

			// Modify the node
			modifiedNode := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status:     v1.NodeStatus{Phase: v1.NodeRunning},
			}
			fakeWatcher.Modify(modifiedNode)

			Eventually(func() v1.NodePhase {
				nodes := watcher.GetNodes()
				if len(nodes.Items) > 0 {
					return nodes.Items[0].Status.Phase
				}
				return ""
			}, time.Second*2, time.Millisecond*100).Should(Equal(v1.NodeRunning))
		})

		It("should handle Deleted events", func() {
			go watcher.watchNodes(ctx)

			// Add nodes
			node1 := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
			node2 := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}
			fakeWatcher.Add(node1)
			fakeWatcher.Add(node2)

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(2))

			// Delete one node
			fakeWatcher.Delete(node1)

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(1))

			nodes := watcher.GetNodes()
			Expect(nodes.Items[0].Name).To(Equal("node2"))
		})

		It("should handle multiple rapid events", func() {
			go watcher.watchNodes(ctx)

			// Rapidly add multiple nodes
			for i := 0; i < 10; i++ {
				node := &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node" + string(rune('0'+i)),
					},
				}
				fakeWatcher.Add(node)
			}

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(10))
		})

		It("should handle Bookmark events without error", func() {
			go watcher.watchNodes(ctx)

			// Send bookmark event (shouldn't cause issues)
			fakeWatcher.Action(watch.Bookmark, &v1.Node{})

			// Add a real node to verify watcher still works
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
			fakeWatcher.Add(node)

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(1))
		})

		It("should stop watching when context is cancelled", func() {
			localCtx, localCancel := context.WithCancel(ctx)

			done := make(chan bool)
			go func() {
				watcher.watchNodes(localCtx)
				done <- true
			}()

			// Add a node
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
			fakeWatcher.Add(node)

			Eventually(func() int {
				return watcher.GetNodeCount()
			}, time.Second*2, time.Millisecond*100).Should(Equal(1))

			// Cancel context
			localCancel()

			// Verify watchNodes exits
			Eventually(done, time.Second*2).Should(Receive())
		})
	})

	Describe("Thread safety", func() {
		It("should handle concurrent reads and writes", func() {
			go watcher.watchNodes(ctx)

			done := make(chan bool)

			// Concurrent writes
			go func() {
				defer func() { done <- true }()
				for i := 0; i < 50; i++ {
					node := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node" + string(rune('0'+i%10)),
						},
					}
					fakeWatcher.Add(node)
					time.Sleep(2 * time.Millisecond)
				}
			}()

			// Concurrent reads
			for i := 0; i < 50; i++ {
				go func() {
					_ = watcher.GetNodes()
					_ = watcher.GetMasterNodes()
					_ = watcher.GetNodeCount()
				}()
			}

			// Wait for writes to complete
			Eventually(done, time.Second*3).Should(Receive())

			// Should not panic and should have nodes
			Expect(watcher.GetNodeCount()).To(BeNumerically(">", 0))
		})
	})
})

var _ = Describe("NodeCacheGetter Interface Compliance", func() {
	It("should implement all NodeCacheGetter methods", func() {
		fakeClientset := fake.NewSimpleClientset()
		watcher := &NodeWatcher{
			client:             fakeClientset,
			nodeList:           &v1.NodeList{},
			watchRetryInterval: 100 * time.Millisecond,
		}

		// Verify the watcher implements the interface
		var _ interface {
			GetNodes() *v1.NodeList
			GetMasterNodes() *v1.NodeList
			GetNodeCount() int
		} = watcher

		// Verify methods work
		nodes := watcher.GetNodes()
		Expect(nodes).NotTo(BeNil())

		masters := watcher.GetMasterNodes()
		Expect(masters).NotTo(BeNil())

		count := watcher.GetNodeCount()
		Expect(count).To(Equal(0))
	})
})
