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
	"net/http"
	"slices"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
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
