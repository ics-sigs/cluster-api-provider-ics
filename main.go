/*
Copyright 2024 The Kubernetes Authors.

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
	"fmt"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"reflect"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/meta"
	cliflag "k8s.io/component-base/cli/flag"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlsig "sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/ics-sigs/cluster-api-provider-ics/api/v1beta1"
	"github.com/ics-sigs/cluster-api-provider-ics/controllers"
	"github.com/ics-sigs/cluster-api-provider-ics/feature"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/constants"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/context"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/manager"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/version"
)

var (
	setupLog = ctrllog.Log.WithName("entrypoint")

	managerOpts     manager.Options
	syncPeriod      time.Duration
	profilerAddress string
	tlsMinVersion   string

	defaultProfilerAddr      = os.Getenv("PROFILER_ADDR")
	defaultSyncPeriod        = manager.DefaultSyncPeriod
	defaultLeaderElectionID  = manager.DefaultLeaderElectionID
	defaultPodName           = manager.DefaultPodName
	defaultWebhookPort       = manager.DefaultWebhookServiceContainerPort
	defaultEnableKeepAlive   = constants.DefaultEnableKeepAlive
	defaultKeepAliveDuration = constants.DefaultKeepAliveDuration
	defaultTLSMinVersion     = "1.2"
)

// InitFlags initializes the flags.
func InitFlags(fs *pflag.FlagSet) {
	flag.StringVar(
		&managerOpts.MetricsBindAddress,
		"metrics-addr",
		":8080",
		"The address the metric endpoint binds to.")
	flag.BoolVar(
		&managerOpts.LeaderElection,
		"enable-leader-election",
		true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(
		&managerOpts.LeaderElectionID,
		"leader-election-id",
		defaultLeaderElectionID,
		"Name of the config map to use as the locking resource when configuring leader election.")
	flag.StringVar(
		&managerOpts.Namespace,
		"namespace",
		"",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")
	flag.StringVar(
		&profilerAddress,
		"profiler-address",
		defaultProfilerAddr,
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")
	flag.DurationVar(
		&syncPeriod,
		"sync-period",
		defaultSyncPeriod,
		"The interval at which cluster-api objects are synchronized")
	flag.IntVar(
		&managerOpts.MaxConcurrentReconciles,
		"max-concurrent-reconciles",
		10,
		"The maximum number of allowed, concurrent reconciles.")
	flag.StringVar(
		&managerOpts.PodName,
		"pod-name",
		defaultPodName,
		"The name of the pod running the controller manager.")
	flag.IntVar(
		&managerOpts.Port,
		"webhook-port",
		9443,
		"Webhook Server port (set to 0 to disable)")
	flag.StringVar(
		&managerOpts.CertDir,
		"webhook-cert-dir",
		"/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir, only used when webhook-port is specified.")
	flag.StringVar(
		&managerOpts.HealthProbeBindAddress,
		"health-addr",
		":9440",
		"The address the health endpoint binds to.",
	)
	flag.BoolVar(
		&managerOpts.EnableKeepAlive,
		"enable-keep-alive",
		defaultEnableKeepAlive,
		"feature to enable keep alive handler in ics sessions. This functionality is enabled by default now",
	)
	flag.DurationVar(
		&managerOpts.KeepAliveDuration,
		"keep-alive-duration",
		defaultKeepAliveDuration,
		"idle time interval(minutes) in between send() requests in keepalive handler",
	)
	flag.StringVar(
		&tlsMinVersion,
		"tls-min-version",
		defaultTLSMinVersion,
		"minimum TLS version in use by the webhook server. Possible values are  \"\", \"1.0\", \"1.1\", \"1.2\" and \"1.3\".",
	)

	feature.MutableGates.AddFlag(fs)
}

func main() {
	rand.Seed(time.Now().UnixNano())
	klog.InitFlags(nil)

	InitFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	if err := pflag.CommandLine.Set("v", "2"); err != nil {
		setupLog.Error(err, "failed to set log level: %v")
		os.Exit(1)
	}
	pflag.Parse()

	ctrllog.SetLogger(klogr.New())
	if err := flag.Set("v", "2"); err != nil {
		klog.Fatalf("failed to set log level: %v", err)
	}

	if managerOpts.Namespace != "" {
		setupLog.Info(
			"Watching objects only in namespace for reconciliation",
			"namespace", managerOpts.Namespace)
	}

	if profilerAddress != "" {
		setupLog.Info(
			"Profiler listening for requests",
			"profiler-address", profilerAddress)
		go runProfiler(profilerAddress)
	}
	setupLog.V(1).Info(fmt.Sprintf("feature gates: %+v\n", feature.Gates))

	managerOpts.SyncPeriod = &syncPeriod

	// Create a function that adds all the controllers and webhooks to the manager.
	addToManager := func(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
		cluster := &v1beta1.ICSCluster{}
		gvr := v1beta1.GroupVersion.WithResource(reflect.TypeOf(cluster).Elem().Name())
		_, err := mgr.GetRESTMapper().KindFor(gvr)
		if err != nil {
			if meta.IsNoMatchError(err) {
				setupLog.Info(fmt.Sprintf("CRD for %s not loaded, skipping.", gvr.String()))
			} else {
				return err
			}
		} else {
			if err := setupControllers(ctx, mgr); err != nil {
				return err
			}
			if err := setupWebhooks(mgr); err != nil {
				return err
			}
		}

		if meta.IsNoMatchError(err) {
			setupLog.Info(fmt.Sprintf("CRD for %s not loaded, skipping.", gvr.String()))
		} else {
			return err
		}

		return nil
	}

	setupLog.Info("creating controller manager", "version", version.Get().String())
	managerOpts.AddToManager = addToManager
	mgr, err := manager.New(managerOpts)
	if err != nil {
		setupLog.Error(err, "problem creating controller manager")
		os.Exit(1)
	}

	mgr.GetWebhookServer().TLSMinVersion = tlsMinVersion
	setupChecks(mgr)

	sigHandler := ctrlsig.SetupSignalHandler()
	setupLog.Info("starting controller manager")
	if err := mgr.Start(sigHandler); err != nil {
		setupLog.Error(err, "problem running controller manager")
		os.Exit(1)
	}
}

func setupControllers(ctx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
	if err := controllers.AddClusterControllerToManager(ctx, mgr, &v1beta1.ICSCluster{}); err != nil {
		return err
	}
	if err := controllers.AddMachineControllerToManager(ctx, mgr, &v1beta1.ICSMachine{}); err != nil {
		return err
	}
	if err := controllers.AddVMControllerToManager(ctx, mgr); err != nil {
		return err
	}
	if err := controllers.AddIPAddressControllerToManager(ctx, mgr); err != nil {
		return err
	}
	return nil
}

func setupWebhooks(mgr ctrlmgr.Manager) error {
	if err := (&v1beta1.ICSCluster{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}
	if err := (&v1beta1.ICSClusterList{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}

	if err := (&v1beta1.ICSMachine{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}
	if err := (&v1beta1.ICSMachineList{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}

	if err := (&v1beta1.ICSMachineTemplate{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}
	if err := (&v1beta1.ICSMachineTemplateList{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}

	if err := (&v1beta1.ICSVM{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}
	if err := (&v1beta1.ICSVMList{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}

	if err := (&v1beta1.IPAddress{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}
	if err := (&v1beta1.IPAddressList{}).SetupWebhookWithManager(mgr); err != nil {
		return err
	}
	return nil
}

func setupChecks(mgr ctrlmgr.Manager) {
	if err := mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}
}

func runProfiler(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	_ = http.ListenAndServe(addr, mux)
}
