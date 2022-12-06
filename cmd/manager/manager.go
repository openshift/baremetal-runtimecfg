/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"
	"time"

	"github.com/spf13/pflag"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/config"
	"k8s.io/component-base/config/options"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"context"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/library-go/pkg/config/clusterstatus"
	"github.com/openshift/library-go/pkg/config/leaderelection"
	"k8s.io/client-go/rest"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	leaderElectionConfig = config.LeaderElectionConfiguration{
		LeaderElect:  true,
		ResourceName: "baremetal-runtimecfg-leader",
	}
)

const (
	DefaultManagedNamespace         = "openshift-baremetal-runtimecfg"
	OpenshiftConfigNamespace        = "openshift-config"
	OpenshiftManagedConfigNamespace = "openshift-config-managed"
)

// getLeaderElectionDefaults returns leader election configs defaults based on the cluster topology
func getLeaderElectionDefaults(restConfig *rest.Config, leaderElection configv1.LeaderElection) configv1.LeaderElection {

	userExplicitlySetLeaderElectionValues := leaderElection.LeaseDuration.Duration != 0 ||
		leaderElection.RenewDeadline.Duration != 0 ||
		leaderElection.RetryPeriod.Duration != 0

	// Defaults follow conventions
	// https://github.com/openshift/enhancements/blob/master/CONVENTIONS.md#high-availability
	defaultLeaderElection := leaderelection.LeaderElectionDefaulting(
		leaderElection,
		"", "",
	)

	// If user has not supplied any leader election values and leader election is not disabled
	// Fetch cluster infra status to determine if we should be using SNO LE config
	if !userExplicitlySetLeaderElectionValues && !leaderElection.Disable {
		if infra, err := clusterstatus.GetClusterInfraStatus(context.TODO(), restConfig); err == nil && infra != nil {
			if infra.ControlPlaneTopology == configv1.SingleReplicaTopologyMode {
				return leaderelection.LeaderElectionSNOConfig(defaultLeaderElection)
			}
		} else {
			klog.Warningf("unable to get cluster infrastructure status, using HA cluster values for leader election: %v", err)
		}
	}

	return defaultLeaderElection
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	klog.InitFlags(nil)

	healthAddr := flag.String(
		"health-addr",
		":9440",
		"The address for health checking.",
	)

	managedNamespace := flag.String(
		"namespace",
		DefaultManagedNamespace,
		"The namespace for managed objects.",
	)

	// Once all the flags are registered, switch to pflag
	// to allow leader lection flags to be bound
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	options.BindLeaderElectionFlags(&leaderElectionConfig, pflag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(klogr.New().WithName("BaremetalRuntimeCfgControllers"))

	restConfig := ctrl.GetConfigOrDie()
	le := getLeaderElectionDefaults(restConfig, configv1.LeaderElection{
		Disable:       !leaderElectionConfig.LeaderElect,
		RenewDeadline: leaderElectionConfig.RenewDeadline,
		RetryPeriod:   leaderElectionConfig.RetryPeriod,
		LeaseDuration: leaderElectionConfig.LeaseDuration,
	})

	syncPeriod := 10 * time.Minute
	cacheBuilder := cache.MultiNamespacedCacheBuilder([]string{
		*managedNamespace, OpenshiftConfigNamespace, OpenshiftManagedConfigNamespace,
	})
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Namespace:               *managedNamespace,
		Scheme:                  scheme,
		SyncPeriod:              &syncPeriod,
		MetricsBindAddress:      "0", // we do not expose any metric at this point
		HealthProbeBindAddress:  *healthAddr,
		LeaderElectionNamespace: leaderElectionConfig.ResourceNamespace,
		LeaderElection:          leaderElectionConfig.LeaderElect,
		LeaderElectionID:        leaderElectionConfig.ResourceName,
		LeaseDuration:           &le.LeaseDuration.Duration,
		RetryPeriod:             &le.RetryPeriod.Duration,
		RenewDeadline:           &le.RenewDeadline.Duration,
		NewCache:                cacheBuilder,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.CloudConfigReconciler{
		ClusterOperatorStatusClient: controllers.ClusterOperatorStatusClient{
			Client:           mgr.GetClient(),
			Recorder:         mgr.GetEventRecorderFor("baremetal-rutimecfg-controller"),
			ReleaseVersion:   controllers.GetReleaseVersion(),
			ManagedNamespace: *managedNamespace,
		},
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create baremetal-runtimecfg controller", "controller", "ClusterOperator")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
