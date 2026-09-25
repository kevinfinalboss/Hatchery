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
	var allowedImageRegistries string
	var redisURL, trustedProxies string
	var backupEndpoint, backupBucket, backupSecret string
	var uiDir string
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
	flag.StringVar(&allowedImageRegistries, "allowed-image-registries", os.Getenv("PANEL_ALLOWED_IMAGE_REGISTRIES"),
		"Comma-separated default registries for organizations' private Eggs (same list as the operator's). Empty: no check.")
	flag.StringVar(&redisURL, "redis-url", os.Getenv("PANEL_REDIS_URL"),
		"Redis URL (redis:// or rediss:// for TLS, password in the URL) for console tickets and the login rate limiter. "+
			"Required. Redis 6.2 or newer (GETDEL). Defaults to $PANEL_REDIS_URL.")
	flag.StringVar(&trustedProxies, "trusted-proxies", os.Getenv("PANEL_TRUSTED_PROXIES"),
		"Comma-separated CIDRs of reverse proxies whose X-Forwarded-For header is trusted when working out the client IP "+
			"(rate limiting, audit). Empty trusts no proxy. Defaults to $PANEL_TRUSTED_PROXIES.")
	flag.StringVar(&backupEndpoint, "backup-s3-endpoint", os.Getenv("PANEL_BACKUP_S3_ENDPOINT"),
		"Endpoint of the platform's own S3-compatible backup storage (empty for AWS S3). Defaults to $PANEL_BACKUP_S3_ENDPOINT.")
	flag.StringVar(&backupBucket, "backup-s3-bucket", os.Getenv("PANEL_BACKUP_S3_BUCKET"),
		"Bucket of the platform's backup storage. Empty turns the platform storage off (organizations can still bring their own S3). "+
			"Defaults to $PANEL_BACKUP_S3_BUCKET.")
	flag.StringVar(&backupSecret, "backup-s3-secret", os.Getenv("PANEL_BACKUP_S3_SECRET"),
		"<namespace>/<name> of the Secret holding the platform's S3 access-key and secret-key. Defaults to $PANEL_BACKUP_S3_SECRET.")
	flag.StringVar(&uiDir, "ui-dir", os.Getenv("PANEL_UI_DIR"),
		"Directory with the built web UI (web/dist) to serve on every non-API path. Empty serves no UI (local dev uses Vite). "+
			"Defaults to $PANEL_UI_DIR.")
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

	if err := panelapi.BootstrapAdmin(ctx, c, db, adminSecretNamespace, adminSecretName, "admin@hatchery.local"); err != nil {
		log.Error(err, "failed to bootstrap admin user")
		os.Exit(1)
	}
	log.Info("admin bootstrap checked", "secretNamespace", adminSecretNamespace, "secretName", adminSecretName,
		"note", "if this is the first run, fetch the generated password from that Secret")

	srv := panelapi.NewServer(c, clientset, cfg, db, sftpAgentImage, splitList(allowedOrigins))
	srv.AllowedImageRegistries = splitList(allowedImageRegistries)

	srv.Tickets = panelcache.NewRedisTicketStore(rdb)
	srv.LoginLimiter = panelcache.NewRedisLoginLimiter(rdb, panelcache.DefaultLoginLimits)
	srv.TrustedProxies = proxyNets
	srv.UIDir = uiDir

	if backupBucket != "" {
		ns, name, ok := strings.Cut(backupSecret, "/")
		if !ok || ns == "" || name == "" {
			log.Error(nil, "--backup-s3-bucket needs --backup-s3-secret=<namespace>/<name>")
			os.Exit(1)
		}
		srv.Backup = panelapi.BackupConfig{Endpoint: backupEndpoint, Bucket: backupBucket, SecretNamespace: ns, SecretName: name}
		log.Info("platform backup storage enabled", "bucket", backupBucket, "endpoint", backupEndpoint)
	}

	metricsStore := panelcache.NewRedisMetricsStore(rdb)
	srv.Metrics = metricsStore
	go panelapi.NewMetricsSampler(c, clientset, metricsStore).Run(ctx)

	log.Info("starting panel-api", "bindAddress", bindAddr)
	if err := http.ListenAndServe(bindAddr, srv.Routes()); err != nil {
		log.Error(err, "panel-api server stopped")
		os.Exit(1)
	}
}

// splitList parses a comma-separated flag value into a trimmed, non-empty slice (nil for an
// empty input). Shared by --allowed-origins and --allowed-image-registries.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
