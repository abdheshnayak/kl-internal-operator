package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kloudlite/internal_operator_v2/env"
	"github.com/kloudlite/internal_operator_v2/lib/logging"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	managementv1 "github.com/kloudlite/internal_operator_v2/apis/management/v1"
	account "github.com/kloudlite/internal_operator_v2/controllers/common/account"

	infrav1 "github.com/kloudlite/internal_operator_v2/apis/infra/v1"
	commoncontroller "github.com/kloudlite/internal_operator_v2/controllers/common"
	infracontrollers "github.com/kloudlite/internal_operator_v2/controllers/infra"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(managementv1.AddToScheme(scheme))
	utilruntime.Must(infrav1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func fromEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		panic(fmt.Errorf("ENV '%v' is not provided", key))
	}
	return value
}

func main() {

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9091", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9092", "The address the probe endpoint binds to.")

	flag.BoolVar(
		&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)

	var isDev bool
	flag.BoolVar(&isDev, "dev", false, "Enable development mode")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	envVars := env.GetEnvOrDie()

	var mgr manager.Manager
	if isDev {
		mr, err := ctrl.NewManager(
			&rest.Config{
				Host: "localhost:8080",
			},
			ctrl.Options{
				Scheme:                     scheme,
				MetricsBindAddress:         metricsAddr,
				Port:                       9443,
				HealthProbeBindAddress:     probeAddr,
				LeaderElection:             enableLeaderElection,
				LeaderElectionID:           "internal-operator.kloudlite.io",
				LeaderElectionResourceLock: "configmaps",
			},
		)
		if err != nil {
			setupLog.Error(err, "unable to start manager")
			os.Exit(1)
		}
		mgr = mr
	} else {
		mr, err := ctrl.NewManager(
			ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                     scheme,
				MetricsBindAddress:         metricsAddr,
				Port:                       9443,
				HealthProbeBindAddress:     probeAddr,
				LeaderElection:             enableLeaderElection,
				LeaderElectionID:           "internal-operator.kloudlite.io",
				LeaderElectionResourceLock: "configmaps",
			},
		)
		if err != nil {
			setupLog.Error(err, "unable to start manager")
			os.Exit(1)
		}
		mgr = mr
	}

	func() {
		if os.Getenv("COMM") != "true" {
			return
		}

		logger := logging.NewOrDie(&logging.Options{Dev: true})

		if err := (&account.AccountReconciler{
			Env:  envVars,
			Name: "account",
		}).SetupWithManager(mgr, logger); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "KeyPrefix")
			os.Exit(1)
		}

		// return

		if err := (&commoncontroller.DomainReconciler{
			Env:  envVars,
			Name: "device",
		}).SetupWithManager(mgr, logger); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "KeyPrefix")
			os.Exit(1)
		}

		if err := (&commoncontroller.RegionReconciler{
			Env:  envVars,
			Name: "region",
		}).SetupWithManager(mgr, logger); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Region")
			os.Exit(1)
		}

		if err := (&commoncontroller.DeviceReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Env:    envVars,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "KeyPrefix")
			os.Exit(1)
		}
	}()

	func() {
		if os.Getenv("INFRA") != "true" {
			return
		}

		logger := logging.NewOrDie(&logging.Options{Dev: true})

		if err := (&infracontrollers.CloudProviderReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Name:   "CLProvider",
		}).SetupWithManager(mgr, logger); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "CloudProvider")
			os.Exit(1)
		}

		if err := (&infracontrollers.NodePoolReconciler{
			Name:   "nodepool",
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr, logger); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "NodePool")
			os.Exit(1)
		}

		if err := (&infracontrollers.AccountNodeReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Env:    envVars,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "AccountNode")
			os.Exit(1)
		}

		if err := (&infracontrollers.EdgeReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Name:   "Edge",
		}).SetupWithManager(mgr, logger); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "AccountProvider")
			os.Exit(1)
		}
	}()

	// +kubebuilder:scaffold:builder

	var err error

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		panic(err)
	}
}
