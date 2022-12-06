package controllers

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	infrastructureResourceName = "cluster"
)

type BGPNodeConfigReconciler struct {
	k8sClient client.Client
}

// If the BGP configuration in Infrastructure changes we want to render all control plane nodes
// If Addresses changes on a control plane node we want to render that node
// Watches pods owned by us

// TODO: Need a namespace

// Inputs:
//   Infrastructure/BGP
//   Node/Addresses
// State:
//   ConfigMap/frr-<node>
//     Annotation: baremetal-runtimecfg-frr-generation-written=X
//   Pod/copy-frr-<node>
//     Annotation: baremetal-runtimecfg-frr-generation=X

// Controller reconciles Node:
//   Fetch Node
//   Fetch BGP config
//   Render frr.conf for Node using above
//   Create or update frr.conf in ConfigMap frr-<node>
//   If updated:
//     exit, wait for re-reconcile
//   If ConfigMap annotation == current generation -> success
//
//   Pod name: copy-frr-conf-<node>
//   If pod exists:
//      Check annotation for current ConfigMap generation
//      if annotation matches:
//         if pod status success:
//            annotate ConfigMap with ConfigMap generation (e.g. generation-written=6)
//         if pod status failed:
//            delete pod
//      else:
//         delete pod
//      exit, wait for re-reconcile
//
//   Create pod, annotated with ConfigMap generation
//   exit, wait for re-reconcile

func (r *BGPNodeConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("Syncing cloud-conf ConfigMap")

	infra := &configv1.Infrastructure{}
	if err := r.k8sClient.Get(ctx, client.ObjectKey{Name: infrastructureResourceName}, infra); err != nil {
		klog.Errorf("infrastructure resource not found")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BGPNodeConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	build := ctrl.NewControllerManagedBy(mgr).
		For(
			&corev1.Node{},
		).
		Watches(
			&source.Kind{Type: &configv1.Infrastructure{}},
			handler.EnqueueRequestsFromMapFunc(func(_ client.Object) []reconcile.Request {
				k8sclient := mgr.GetClient()
				nodes := corev1.NodeList{}
				k8sclient.List(context.TODO(), &nodes)

				var requests []reconcile.Request
				for _, node := range nodes.Items {
					req := reconcile.Request{}
					req.Name = node.Name
					requests = append(requests, req)
				}
				return requests
			}),
		)

	return build.Complete(r)
}
