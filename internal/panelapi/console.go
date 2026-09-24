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
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/panelcache"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients (server-to-server, CLI tools) never send an
		// Origin header at all; only a browser does, and only a browser
		// needs this check.
		return true
	}
	if len(s.AllowedOrigins) == 0 {
		return true
	}
	return slices.Contains(s.AllowedOrigins, origin)
}

// consoleTicketTTL is how long a console ticket can wait to be used. It only
// has to cover the round trip between the client asking for it and opening
// the WebSocket, so it is short: a leaked ticket is worthless almost at once.
const consoleTicketTTL = 30 * time.Second

var consoleLog = logf.Log.WithName("panelapi-console")

func (s *Server) handleConsoleTicket(w http.ResponseWriter, r *http.Request) {
	acc := orgAccessFromContext(r.Context())
	ticket, err := s.Tickets.Issue(r.Context(), panelcache.ConsoleTicket{
		UserID:     userFromContext(r.Context()).ID,
		Org:        acc.Org.Slug,
		GameServer: r.PathValue("name"),
	}, consoleTicketTTL)
	if err != nil {
		consoleLog.Error(err, "could not issue console ticket")
		writeError(w, http.StatusServiceUnavailable, "console is temporarily unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":           ticket,
		"expiresInSeconds": int(consoleTicketTTL.Seconds()),
	})
}

func (s *Server) requireConsoleTicket(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticket := r.URL.Query().Get("ticket")
		if ticket == "" {
			writeError(w, http.StatusUnauthorized, "missing console ticket")
			return
		}
		t, err := s.Tickets.Consume(r.Context(), ticket)
		if err != nil {
			if errors.Is(err, panelcache.ErrTicketNotFound) {
				writeError(w, http.StatusUnauthorized, "invalid or expired console ticket")
				return
			}
			consoleLog.Error(err, "could not consume console ticket")
			writeError(w, http.StatusServiceUnavailable, "console is temporarily unavailable")
			return
		}
		if t.Org != r.PathValue("org") || t.GameServer != r.PathValue("name") {
			writeError(w, http.StatusUnauthorized, "invalid or expired console ticket")
			return
		}

		user, err := s.DB.GetUser(r.Context(), t.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired console ticket")
			return
		}
		acc, status, msg := s.resolveOrgAccess(r.Context(), user, t.Org, paneldb.RoleMember)
		if acc == nil {
			writeError(w, status, msg)
			return
		}
		if !acc.Can(t.GameServer, paneldb.PermConsoleWrite) {
			writeError(w, http.StatusForbidden, "you no longer have console access to this server")
			return
		}

		r.SetPathValue("namespace", gameserversv1alpha1.TenantNamespace(acc.Org.Slug))
		ctx := context.WithValue(withOrgAccess(r.Context(), acc), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleConsole attaches to the GameServer's already-running "server"
// container (pods/attach, not pods/exec — this talks to the process already
// running as PID 1, the same one game clients are connected to) and bridges
// its stdio to a WebSocket connection: every chunk the process writes becomes
// one binary WebSocket message out, and every message the client sends
// becomes bytes on the process's stdin. This only works because the
// GameServer controller creates the container with Stdin: true — see the
// comment in buildPod.
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	upgrader := websocket.Upgrader{CheckOrigin: s.checkOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	req := s.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(name).
		SubResource("attach").
		VersionedParams(&corev1.PodAttachOptions{
			Container: "server",
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.RESTConfig, http.MethodPost, req.URL())
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		return
	}

	rw := &wsReadWriter{conn: conn}
	if err := executor.StreamWithContext(r.Context(), remotecommand.StreamOptions{
		Stdin:  rw,
		Stdout: rw,
		Stderr: rw,
	}); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
	}
}

// wsReadWriter adapts a *websocket.Conn to io.Reader/io.Writer so it can be
// wired directly into remotecommand's Stdin/Stdout/Stderr. Write sends one
// binary WebSocket message per call; Read drains the current inbound message
// before blocking for the next one, so a client message larger than the
// caller's buffer is delivered across several Read calls without losing
// bytes.
type wsReadWriter struct {
	conn *websocket.Conn
	rest []byte
}

func (w *wsReadWriter) Read(p []byte) (int, error) {
	for len(w.rest) == 0 {
		_, data, err := w.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		w.rest = data
	}
	n := copy(p, w.rest)
	w.rest = w.rest[n:]
	return n, nil
}

func (w *wsReadWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
