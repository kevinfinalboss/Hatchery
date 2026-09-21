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
	"flag"
	"net/http"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelapi"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func main() {
	var bindAddr string
	var sftpAgentImage string
	var postgresDSN string
	var adminSecretNamespace string
	var adminSecretName string
	var allowedOrigins string
	var redisURL, trustedProxies string
	flag.StringVar(&bindAddr, "bind-address", ":8090", "Address the Panel API HTTP server binds to.")
	flag.StringVar(&sftpAgentImage, "sftp-agent-image", "hatchery/sftp-agent:dev",
		"Container image used for the on-demand SFTP maintenance Pod created for a Stopped GameServer.")
	flag.StringVar(&postgresDSN, "postgres-dsn", os.Getenv("POSTGRES_DSN"),
		"Postgres connection string for the Panel's users/sessions/permissions database. Defaults to $POSTGRES_DSN.")
	flag.StringVar(&adminSecretNamespace, "admin-secret-namespace", "default",
		"Namespace the bootstrap admin credentials Secret is created in.")
	flag.StringVar(&adminSecretName, "admin-secret-name", "panel-admin-credentials",
		"Name of the bootstrap admin credentials Secret (mirrors ArgoCD's argocd-initial-admin-secret).")
	flag.StringVar(&allowedOrigins, "allowed-origins", os.Getenv("PANEL_ALLOWED_ORIGINS"),
		"Comma-separated allowlist of Origins accepted by the console WebSocket (e.g. https://panel.example.com). "+
			"Empty accepts any Origin, matching pre-allowlist behavior. Defaults to $PANEL_ALLOWED_ORIGINS.")
	flag.StringVar(&redisURL, "redis-url", os.Getenv("PANEL_REDIS_URL"),
		"Redis URL (redis:// or rediss:// for TLS, password in the URL) for console tickets and the login rate limiter. "+
			"Required. Redis 6.2 or newer (GETDEL). Defaults to $PANEL_REDIS_URL.")
	flag.StringVar(&trustedProxies, "trusted-proxies", os.Getenv("PANEL_TRUSTED_PROXIES"),
		"Comma-separated CIDRs of reverse proxies whose X-Forwarded-For header is trusted when working out the client IP "+
			"(rate limiting, audit). Empty trusts no proxy. Defaults to $PANEL_TRUSTED_PROXIES.")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("setup")

	if postgresDSN == "" {
		log.Error(nil, "--postgres-dsn (or $POSTGRES_DSN) is required")
		os.Exit(1)
	}

	if redisURL == "" {
		log.Error(nil, "--redis-url (or $PANEL_REDIS_URL) is required")
		os.Exit(1)
	}
	proxyNets, err := panelapi.ParseCIDRs(trustedProxies)
	if err != nil {
		log.Error(err, "invalid --trusted-proxies")
		os.Exit(1)
	}

	ctx := context.Background()

	sqlDB, err := paneldb.Open(ctx, postgresDSN)
	if err != nil {
		log.Error(err, "failed to connect to postgres")
		os.Exit(1)
	}
	if err := paneldb.Migrate(ctx, sqlDB); err != nil {
		log.Error(err, "failed to run database migrations")
		os.Exit(1)
	}
	db := paneldb.NewStore(sqlDB)

	rdb, err := panelcache.OpenRedis(ctx, redisURL)
	if err != nil {
		log.Error(err, "failed to connect to redis")
		os.Exit(1)
	}
	defer rdb.Close()

	// ctrl.GetConfig follows the same resolution order the operator binary
	// already relies on: in-cluster config when running as a Pod, otherwise
	// KUBECONFIG / the current context in ~/.kube/config for local dev.
	cfg, err := ctrl.GetConfig()
	if err != nil {
		log.Error(err, "failed to load Kubernetes config")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gameserversv1alpha1.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Error(err, "failed to build Kubernetes client")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Error(err, "failed to build Kubernetes clientset")
		os.Exit(1)
	}

	if err := panelapi.BootstrapAdmin(ctx, c, db, adminSecretNamespace, adminSecretName); err != nil {
		log.Error(err, "failed to bootstrap admin user")
		os.Exit(1)
	}
	log.Info("admin bootstrap checked", "secretNamespace", adminSecretNamespace, "secretName", adminSecretName,
		"note", "if this is the first run, fetch the generated password from that Secret")

	var originAllowlist []string
	if allowedOrigins != "" {
		for _, o := range strings.Split(allowedOrigins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				originAllowlist = append(originAllowlist, o)
			}
		}
	}

	srv := panelapi.NewServer(c, clientset, cfg, db, sftpAgentImage, originAllowlist)

	srv.Tickets = panelcache.NewRedisTicketStore(rdb)
	srv.LoginLimiter = panelcache.NewRedisLoginLimiter(rdb, panelcache.DefaultLoginLimits)
	srv.TrustedProxies = proxyNets

	metricsStore := panelcache.NewRedisMetricsStore(rdb)
	srv.Metrics = metricsStore
	go panelapi.NewMetricsSampler(c, clientset, metricsStore).Run(ctx)

	log.Info("starting panel-api", "bindAddress", bindAddr)
	if err := http.ListenAndServe(bindAddr, srv.Routes()); err != nil {
		log.Error(err, "panel-api server stopped")
		os.Exit(1)
	}
}
