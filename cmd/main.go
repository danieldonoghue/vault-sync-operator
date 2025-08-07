// Package main is the entry point for the vault-sync-operator.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"

	"github.com/danieldonoghue/vault-sync-operator/internal/controller"
	"github.com/danieldonoghue/vault-sync-operator/internal/goruntime"
	_ "github.com/danieldonoghue/vault-sync-operator/internal/metrics" // Initialize metrics
	"github.com/danieldonoghue/vault-sync-operator/internal/vault"

	// Import automaxprocs to automatically set GOMAXPROCS based on container limits.
	_ "go.uber.org/automaxprocs"
)

var (
	// Build-time variables (set via ldflags).
	version = "dev"
	commit  = "unknown"
	date    = "unknown"

	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var vaultAddr string
	var vaultRole string
	var vaultAuthPath string
	var clusterName string
	var showVersion bool
	var enableMetricsAuth bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableMetricsAuth, "enable-metrics-auth", true,
		"Enable authentication and authorization for metrics endpoint. "+
			"Set to false to disable authentication (not recommended for production).")
	flag.StringVar(&vaultAddr, "vault-addr", "http://vault:8200", "Vault server address")
	flag.StringVar(&vaultRole, "vault-role", "vault-sync-operator", "Vault Kubernetes auth role")
	flag.StringVar(&vaultAuthPath, "vault-auth-path", "kubernetes", "Vault Kubernetes auth path")
	flag.StringVar(&clusterName, "cluster-name", "", "Optional cluster name for multi-cluster Vault path organization")
	flag.BoolVar(&showVersion, "version", false, "Show version information and exit")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Printf("vault-sync-operator version %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Log version information at startup
	setupLog.Info("starting vault-sync-operator",
		"version", version,
		"commit", commit,
		"build_date", date)

	// Log Go runtime configuration for container awareness
	goruntime.LogRuntimeConfiguration(setupLog)
	goruntime.ValidateRuntimeConfiguration(setupLog)

	// Configure metrics options based on authentication setting
	metricsOptions := metricsserver.Options{
		BindAddress: metricsAddr,
	}
	if enableMetricsAuth {
		setupLog.Info("metrics authentication enabled")
		metricsOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	} else {
		setupLog.Info("metrics authentication disabled - metrics endpoint will be accessible without authentication")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOptions,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "vault-sync-operator.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize Vault client
	vaultClient, err := vault.NewClient(vaultAddr, vaultRole, vaultAuthPath)
	if err != nil {
		setupLog.Error(err, "unable to initialize vault client")
		os.Exit(1)
	}

	// Log cluster configuration
	if clusterName != "" {
		setupLog.Info("multi-cluster mode enabled", "cluster_name", clusterName, "vault_path_prefix", fmt.Sprintf("clusters/%s/", clusterName))
	} else {
		setupLog.Info("single-cluster mode (no cluster prefix for vault paths)")
	}

	if err = (&controller.DeploymentReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Log:         ctrl.Log.WithName("controllers").WithName("Deployment"),
		VaultClient: vaultClient,
		ClusterName: clusterName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Deployment")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		return vaultClient.HealthCheck(req.Context())
	}); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		return vaultClient.ReadinessCheck(req.Context())
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
