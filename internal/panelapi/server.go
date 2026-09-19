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

// Package panelapi is the HTTP API for the Panel: CRUD over Egg/GameServer
// CRDs, plus log streaming and an interactive console, all talking to the
// Kubernetes API server directly (no Wings-style per-node daemon — see
// AGENTS.md's "Decisão de design: eliminar o 'daemon' para exec/logs/console").
// User accounts, sessions and per-GameServer permission grants live in
// Postgres (internal/paneldb) — see AGENTS.md for why that's a real database
// and not another CRD.
package panelapi

import (
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// Server holds everything the HTTP handlers need.
//
//   - Client is a controller-runtime client for typed CRUD over Egg/GameServer
//     (and anything else registered in its scheme).
//   - Clientset is a plain client-go Clientset for the subresources
//     controller-runtime's client doesn't wrap: pods/log and pods/attach.
//   - RESTConfig is the raw config those subresources need to open their own
//     SPDY connection for the console (remotecommand can't reuse Clientset's
//     already-built REST client for that).
//   - DB is the Panel's own user/session/permission store.
type Server struct {
	Client     client.Client
	Clientset  kubernetes.Interface
	RESTConfig *rest.Config
	DB         *paneldb.Store

	// SFTPAgentImage is the container image used for the on-demand
	// maintenance Pod created when a Stopped GameServer needs an SFTP
	// session — the sidecar case reuses whatever image the
	// GameServerController already put in the Pod.
	SFTPAgentImage string
}

// NewServer builds a Server. cfg and clientset are kept alongside client
// because the console handler needs the raw REST config to build its own SPDY
// executor, and Clientset because controller-runtime's client doesn't expose
// the log/attach subresources.
func NewServer(c client.Client, clientset kubernetes.Interface, cfg *rest.Config, db *paneldb.Store, sftpAgentImage string) *Server {
	return &Server{Client: c, Clientset: clientset, RESTConfig: cfg, DB: db, SFTPAgentImage: sftpAgentImage}
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
	mux.Handle("GET /api/v1/users/{id}/permissions", s.requireAdmin(http.HandlerFunc(s.handleListUserPermissions)))
	mux.Handle("POST /api/v1/users/{id}/permissions", s.requireAdmin(http.HandlerFunc(s.handleGrantUserPermission)))
	mux.Handle("DELETE /api/v1/users/{id}/permissions/{namespace}/{name}", s.requireAdmin(http.HandlerFunc(s.handleRevokeUserPermission)))

	// Eggs are a read-only catalog, not scoped to any user's permissions —
	// any authenticated user can see what's available to build a server
	// from.
	mux.Handle("GET /api/v1/eggs", s.requireAuth(http.HandlerFunc(s.handleListEggs)))
	mux.Handle("GET /api/v1/eggs/{namespace}/{name}", s.requireAuth(http.HandlerFunc(s.handleGetEgg)))

	// Creating/deleting GameServers is provisioning infrastructure —
	// admin-only. Everything scoped to one already-existing GameServer
	// additionally requires a permission grant (or admin) via
	// requireGameServerAccess, checked inside each handler since it needs
	// the {namespace}/{name} path values requireAuth alone doesn't parse.
	mux.Handle("GET /api/v1/gameservers", s.requireAuth(http.HandlerFunc(s.handleListGameServers)))
	mux.Handle("POST /api/v1/gameservers", s.requireAdmin(http.HandlerFunc(s.handleCreateGameServer)))
	mux.Handle("GET /api/v1/gameservers/{namespace}/{name}", s.requireAuth(http.HandlerFunc(s.handleGetGameServer)))
	mux.Handle("DELETE /api/v1/gameservers/{namespace}/{name}", s.requireAdmin(http.HandlerFunc(s.handleDeleteGameServer)))
	mux.Handle("PATCH /api/v1/gameservers/{namespace}/{name}/state", s.requireAuth(http.HandlerFunc(s.handleSetGameServerState)))
	mux.Handle("GET /api/v1/gameservers/{namespace}/{name}/logs", s.requireAuth(http.HandlerFunc(s.handleLogs)))
	mux.Handle("GET /api/v1/gameservers/{namespace}/{name}/console", s.requireAuthWS(http.HandlerFunc(s.handleConsole)))
	mux.Handle("POST /api/v1/gameservers/{namespace}/{name}/sftp-session", s.requireAuth(http.HandlerFunc(s.handleSFTPSession)))

	return mux
}
