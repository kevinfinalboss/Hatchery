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

package panelapi

import (
	"context"
	"net"
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

type Server struct {
	Client     client.Client
	Clientset  kubernetes.Interface
	RESTConfig *rest.Config
	DB         *paneldb.Store

	SFTPAgentImage string

	AllowedOrigins []string

	UIDir string

	// Tickets stores single-use console tickets; LoginLimiter throttles failed
	// logins. Both are backed by Redis in production (cmd/panel-api) and by
	// in-memory fakes in tests. Neither may be nil.
	Tickets      panelcache.TicketStore
	LoginLimiter panelcache.LoginLimiter

	Metrics panelcache.MetricsStore

	Backup BackupConfig

	// TrustedProxies are the peers whose X-Forwarded-For header is believed
	// when working out the client IP (see clientIP in clientip.go).
	TrustedProxies []*net.IPNet

	// recordAuditFn overrides how audit events are stored. nil in production
	// (the events go to Postgres); tests set it to simulate a failing store.
	recordAuditFn func(context.Context, paneldb.AuditEvent) error

	resolveSFTPAddr func(namespace, name string) string

	// execFn overrides how commands run inside a Pod (nil: pods/exec). Tests
	// set it because they have no kube-apiserver to exec through.
	execFn func(ctx context.Context, namespace, pod, container string, cmd []string) (string, error)
	disk   diskCache
}

func NewServer(c client.Client, clientset kubernetes.Interface, cfg *rest.Config, db *paneldb.Store, sftpAgentImage string, allowedOrigins []string) *Server {
	return &Server{Client: c, Clientset: clientset, RESTConfig: cfg, DB: db, SFTPAgentImage: sftpAgentImage, AllowedOrigins: allowedOrigins}
}

// Routes builds the HTTP handler for the Panel API.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.Handle("POST /api/v1/auth/logout", s.requireAuth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))

	mux.Handle("GET /api/v1/users", s.requireAdmin(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("POST /api/v1/users", s.requireAdmin(http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", s.requireAdmin(http.HandlerFunc(s.handleDeleteUser)))

	orgRoute := func(min paneldb.Role, action string, h http.HandlerFunc) http.Handler {
		var inner http.Handler = h
		if action != "" {
			inner = s.audited(action, inner)
		}
		return s.requireOrgRole(min, inner)
	}
	// gsRoute is orgRoute for routes that act on one GameServer: a suspended server is locked for the
	// organization (423 with the admin's reason); platform admins pass.
	gsRoute := func(min paneldb.Role, action string, h http.HandlerFunc) http.Handler {
		var inner http.Handler = s.unlessSuspended(h)
		if action != "" {
			inner = s.audited(action, inner)
		}
		return s.requireOrgRole(min, inner)
	}
	const org = "/api/v1/orgs/{org}"
	const gs = org + "/gameservers/{name}"

	mux.Handle("GET /api/v1/orgs", s.requireAuth(http.HandlerFunc(s.handleListOrgs)))
	mux.Handle("POST /api/v1/orgs", s.requireAdmin(http.HandlerFunc(s.handleCreateOrg)))
	mux.Handle("GET "+org, orgRoute(paneldb.RoleMember, "", s.handleGetOrg))
	mux.Handle("DELETE "+org, orgRoute(paneldb.RoleOwner, "", s.handleDeleteOrg))
	mux.Handle("GET "+org+"/quota", orgRoute(paneldb.RoleMember, "", s.handleGetQuota))
	mux.Handle("PATCH "+org+"/quota", s.requireAdmin(orgRoute(paneldb.RoleOwner, "", s.handleUpdateQuota)))

	mux.Handle("GET "+org+"/members", orgRoute(paneldb.RoleMember, "", s.handleListMembers))
	mux.Handle("POST "+org+"/members", orgRoute(paneldb.RoleAdmin, "", s.handleAddMember))
	mux.Handle("PATCH "+org+"/members/{userId}", orgRoute(paneldb.RoleAdmin, "", s.handleSetMemberRole))
	mux.Handle("DELETE "+org+"/members/{userId}", orgRoute(paneldb.RoleMember, "", s.handleRemoveMember))

	mux.Handle("GET "+org+"/eggs", orgRoute(paneldb.RoleMember, "", s.handleListOrgEggs))
	mux.Handle("GET "+org+"/eggs/{scope}/{name}", orgRoute(paneldb.RoleMember, "", s.handleGetOrgEgg))
	mux.Handle("POST "+org+"/eggs", orgRoute(paneldb.RoleAdmin, "", s.handleCreateOrgEgg))
	mux.Handle("PUT "+org+"/eggs/{name}", orgRoute(paneldb.RoleAdmin, "", s.handleUpdateOrgEgg))
	mux.Handle("DELETE "+org+"/eggs/{name}", orgRoute(paneldb.RoleAdmin, "", s.handleDeleteOrgEgg))

	mux.Handle("GET /api/v1/catalog/eggs", s.requireAuth(http.HandlerFunc(s.handleListCatalogEggs)))
	mux.Handle("POST /api/v1/catalog/eggs", s.requireAdmin(http.HandlerFunc(s.handleCreateCatalogEgg)))
	mux.Handle("PUT /api/v1/catalog/eggs/{name}", s.requireAdmin(http.HandlerFunc(s.handleUpdateCatalogEgg)))
	mux.Handle("DELETE /api/v1/catalog/eggs/{name}", s.requireAdmin(http.HandlerFunc(s.handleDeleteCatalogEgg)))

	mux.Handle("GET "+org+"/gameservers", orgRoute(paneldb.RoleMember, "", s.handleListGameServers))
	mux.Handle("POST "+org+"/gameservers", orgRoute(paneldb.RoleAdmin, "", s.handleCreateGameServer))
	mux.Handle("GET "+gs, orgRoute(paneldb.RoleMember, "", s.handleGetGameServer))
	mux.Handle("PATCH "+gs, gsRoute(paneldb.RoleAdmin, "gameserver.update", s.handleUpdateGameServer))
	mux.Handle("DELETE "+gs, gsRoute(paneldb.RoleAdmin, "gameserver.delete", s.handleDeleteGameServer))
	mux.Handle("PATCH "+gs+"/state", gsRoute(paneldb.RoleMember, "gameserver.state", s.handleSetGameServerState))
	mux.Handle("POST "+gs+"/suspend", s.requireAdmin(orgRoute(paneldb.RoleOwner, "gameserver.suspend", s.handleSuspendGameServer)))
	mux.Handle("POST "+gs+"/unsuspend", s.requireAdmin(orgRoute(paneldb.RoleOwner, "gameserver.unsuspend", s.handleUnsuspendGameServer)))
	mux.Handle("GET "+org+"/backup-settings", orgRoute(paneldb.RoleMember, "", s.handleGetBackupSettings))
	mux.Handle("PUT "+org+"/backup-connections/{connection}", orgRoute(paneldb.RoleAdmin, "", s.handlePutBackupConnection))
	mux.Handle("DELETE "+org+"/backup-connections/{connection}", orgRoute(paneldb.RoleAdmin, "", s.handleDeleteBackupConnection))
	mux.Handle("GET "+gs+"/backups", gsRoute(paneldb.RoleMember, "", s.handleListBackups))
	mux.Handle("POST "+gs+"/backups", gsRoute(paneldb.RoleAdmin, "backup.create", s.handleCreateBackup))
	mux.Handle("DELETE "+gs+"/backups/{backup}", gsRoute(paneldb.RoleAdmin, "backup.delete", s.handleDeleteBackup))
	mux.Handle("POST "+gs+"/backups/{backup}/restore", gsRoute(paneldb.RoleAdmin, "backup.restore", s.handleRestoreBackup))
	mux.Handle("POST "+gs+"/reinstall", gsRoute(paneldb.RoleAdmin, "gameserver.reinstall", s.handleReinstallGameServer))
	mux.Handle("POST "+gs+"/restart", gsRoute(paneldb.RoleMember, "gameserver.restart", s.handleRestartGameServer))
	mux.Handle("GET "+gs+"/metrics", gsRoute(paneldb.RoleMember, "", s.handleMetrics))
	mux.Handle("GET "+gs+"/runtime", gsRoute(paneldb.RoleMember, "", s.handleRuntime))
	mux.Handle("GET "+gs+"/logs", gsRoute(paneldb.RoleMember, "", s.handleLogs))
	mux.Handle("POST "+gs+"/sftp-session", gsRoute(paneldb.RoleMember, "gameserver.sftp-session", s.handleSFTPSession))

	mux.Handle("GET "+gs+"/files", gsRoute(paneldb.RoleMember, "", s.handleListFiles))
	mux.Handle("GET "+gs+"/files/content", gsRoute(paneldb.RoleMember, "", s.handleGetFileContent))
	mux.Handle("PUT "+gs+"/files/content", gsRoute(paneldb.RoleMember, "file.write", s.handlePutFileContent))
	mux.Handle("POST "+gs+"/files/mkdir", gsRoute(paneldb.RoleMember, "file.mkdir", s.handleMkdir))
	mux.Handle("POST "+gs+"/files/rename", gsRoute(paneldb.RoleMember, "file.rename", s.handleRenameFile))
	mux.Handle("POST "+gs+"/files/delete", gsRoute(paneldb.RoleMember, "file.delete", s.handleDeleteFiles))
	mux.Handle("POST "+gs+"/files/copy", gsRoute(paneldb.RoleMember, "file.copy", s.handleCopyFile))
	mux.Handle("POST "+gs+"/files/upload", gsRoute(paneldb.RoleMember, "file.upload", s.handleUploadFile))
	mux.Handle("GET "+gs+"/files/download", gsRoute(paneldb.RoleMember, "", s.handleDownloadFiles))
	mux.Handle("POST "+gs+"/files/compress", gsRoute(paneldb.RoleMember, "file.compress", s.handleCompressFiles))
	mux.Handle("POST "+gs+"/files/decompress", gsRoute(paneldb.RoleMember, "file.decompress", s.handleDecompressFile))

	mux.Handle("POST "+gs+"/console-ticket", gsRoute(paneldb.RoleMember, "gameserver.console-ticket", s.handleConsoleTicket))
	mux.Handle("GET "+gs+"/console", s.requireConsoleTicket(http.HandlerFunc(s.handleConsole)))

	mux.Handle("GET "+org+"/audit", orgRoute(paneldb.RoleAdmin, "", s.handleListOrgAudit))
	mux.Handle("GET /api/v1/audit", s.requireAdmin(http.HandlerFunc(s.handleListPlatformAudit)))

	if s.UIDir != "" {
		mux.Handle("GET /", uiHandler(s.UIDir))
	}

	return mux
}
