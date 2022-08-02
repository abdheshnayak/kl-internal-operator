package main

import (
	"flag"
	"fmt"
	"os"

	// "google.golang.org/genproto/googleapis/cloud/bigquery/dataexchange/common"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// deviceclusterv1 "operators.kloudlite.io/apis/device-cluster/v1"
	managementv1 "operators.kloudlite.io/apis/management/v1"
	commoncontroller "operators.kloudlite.io/controllers/common"

	// deviceclustercontrollers "operators.kloudlite.io/controllers/device-cluster"
	// management "operators.kloudlite.io/controllers/management"
	// managementcontrollers "operators.kloudlite.io/controllers/management"
	// managementcontrollers "operators.kloudlite.io/controllers/management"
	infrav1 "operators.kloudlite.io/apis/infra/v1"
	infracontrollers "operators.kloudlite.io/controllers/infra"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(managementv1.AddToScheme(scheme))
	// utilruntime.Must(deviceclusterv1.AddToScheme(scheme))
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

	// executationMode := os.Getenv("EXECUTATION_MODE")

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9091", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9092", "The address the probe endpoint binds to.")

	// if executationMode == "management" {
	// 	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9091", "The address the metric endpoint binds to.")
	// 	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9092", "The address the probe endpoint binds to.")

	// }
	// if executationMode == "device" {
	// 	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9093", "The address the metric endpoint binds to.")
	// 	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9094", "The address the probe endpoint binds to.")

	// }
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
				LeaderElectionID:           "bf38d2f9.kloudlite.io",
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
				LeaderElectionID:           "bf38d2f9.kloudlite.io",
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

		if err := (&commoncontroller.AccountReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "KeyPrefix")
			os.Exit(1)
		}

		if err := (&commoncontroller.DomainReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "KeyPrefix")
			os.Exit(1)
		}

		if err := (&commoncontroller.RegionReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Region")
			os.Exit(1)
		}

		if err := (&commoncontroller.DeviceReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "KeyPrefix")
			os.Exit(1)
		}
	}()

	func() {
		if os.Getenv("INFRA") != "true" {
			return
		}

		if err := (&infracontrollers.AccountNodeReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "AccountNode")
			os.Exit(1)
		}
		if err := (&infracontrollers.AccountProviderReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
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
