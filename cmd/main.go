/*
Copyright 2026.

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
	"context"
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/controller"
	webhookv1alpha1 "github.com/kevinfinalboss/Hatchery/internal/webhook/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(gameserversv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var webhookPort int
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var sftpAgentImage string
	var panelServiceAccount, egressExceptCIDRs string
	var publicPortRange, publicHost, publicGatewayNamespace, publicGatewayName, publicGatewayClass string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "Port the webhook server listens on. "+
		"Defaults to 9443. Set -1 to disable the webhook server.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&sftpAgentImage, "sftp-agent-image", "hatchery/sftp-agent:dev",
		"Container image used for the sftp-agent sidecar injected into every Running GameServer's Pod.")
	flag.StringVar(&panelServiceAccount, "panel-service-account", os.Getenv("OPERATOR_PANEL_SERVICE_ACCOUNT"),
		"ServiceAccount the Panel API runs as, as <namespace>/<name>. When set, each tenant namespace gets a RoleBinding for it "+
			"and an ingress rule letting that namespace reach the sftp-agent port. Empty disables both.")
	flag.StringVar(&egressExceptCIDRs, "egress-except-cidrs", os.Getenv("OPERATOR_EGRESS_EXCEPT_CIDRS"),
		"Comma-separated extra CIDRs tenant pods may not reach over internet egress (added to RFC1918, link-local and CGNAT).")
	flag.StringVar(&publicPortRange, "public-port-range", os.Getenv("OPERATOR_PUBLIC_PORT_RANGE"),
		"Public port pool as <min>-<max> (e.g. 30000-40000) for opt-in GameServer public exposure via Gateway API. "+
			"Empty (the default) disables the feature entirely — the GatewayExposureReconciler is not even registered.")
	flag.StringVar(&publicHost, "public-host", os.Getenv("OPERATOR_PUBLIC_HOST"),
		"Hostname or IP shown to players as where to connect once a GameServer is publicly exposed (typically the VPS relay's address).")
	flag.StringVar(&publicGatewayNamespace, "public-gateway-namespace", envOr("OPERATOR_PUBLIC_GATEWAY_NAMESPACE", "hatchery-system"),
		"Namespace of the shared Gateway API Gateway used for public exposure.")
	flag.StringVar(&publicGatewayName, "public-gateway-name", envOr("OPERATOR_PUBLIC_GATEWAY_NAME", "hatchery-public"),
		"Name of the shared Gateway API Gateway used for public exposure.")
	flag.StringVar(&publicGatewayClass, "public-gateway-class", envOr("OPERATOR_PUBLIC_GATEWAY_CLASS", "cilium"),
		"GatewayClassName set on the shared Gateway (the Cilium Gateway API implementation registers the class named \"cilium\").")
	// Development defaults to false so logs are JSON-encoded by default (structured
	// logging, one object per line — what a log aggregator expects). Pass
	// --zap-devel for human-readable console output while developing locally.
	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
		Port:    webhookPort,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "72bce161.hatchery.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "Failed to create the Kubernetes clientset")
		os.Exit(1)
	}
	if err := (&controller.GameServerReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		SFTPAgentImage: sftpAgentImage,
		LogReader:      controller.ClientsetLogReader{Clientset: clientset},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "gameserver")
		os.Exit(1)
	}
	panelNS, panelName, err := parseServiceAccount(panelServiceAccount)
	if err != nil {
		setupLog.Error(err, "invalid --panel-service-account")
		os.Exit(1)
	}
	extraEgressExcept, err := parseCIDRList(egressExceptCIDRs)
	if err != nil {
		setupLog.Error(err, "invalid --egress-except-cidrs")
		os.Exit(1)
	}
	if err := (&controller.TenantReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		PanelServiceAccount: types.NamespacedName{Namespace: panelNS, Name: panelName},
		EgressExceptCIDRs:   extraEgressExcept,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "tenant")
		os.Exit(1)
	}
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return (&controller.CatalogEnsurer{
			Client:              mgr.GetClient(),
			PanelServiceAccount: types.NamespacedName{Namespace: panelNS, Name: panelName},
		}).Ensure(ctx)
	})); err != nil {
		setupLog.Error(err, "Failed to register the catalog ensurer")
		os.Exit(1)
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupGameServerWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "GameServer")
			os.Exit(1)
		}
	}
	if err := (&controller.GameServerBackupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "gameserverbackup")
		os.Exit(1)
	}
	if err := (&controller.GameServerRestoreReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "gameserverrestore")
		os.Exit(1)
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupGameServerRestoreWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create webhook", "webhook", "GameServerRestore")
			os.Exit(1)
		}
	}
	publicPortMin, publicPortMax, err := parsePublicPortRange(publicPortRange)
	if err != nil {
		setupLog.Error(err, "invalid --public-port-range")
		os.Exit(1)
	}
	if publicPortMin != 0 {
		if err := (&controller.GatewayExposureReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			PortRangeMin:     publicPortMin,
			PortRangeMax:     publicPortMax,
			PublicHost:       publicHost,
			GatewayName:      publicGatewayName,
			GatewayNamespace: publicGatewayNamespace,
			GatewayClassName: publicGatewayClass,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create controller", "controller", "gatewayexposure")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}
