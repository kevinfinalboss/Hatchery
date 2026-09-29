package panelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

// Context keys only this package can set: a request carrying internalUserKey was built by
// Server.dispatch for an already identified user (the Discord bot), never by an HTTP client.
type internalKey int

const (
	internalUserKey internalKey = iota
	internalViaKey
)

// dispatch runs one of the Panel's own routes as user, in-process, so a caller such as the
// Discord bot gets exactly the panel's permission checks, suspension lock and audit trail. via
// ("discord") is added to the audit metadata.
func (s *Server) dispatch(ctx context.Context, user *paneldb.User, via, method, path string, body any) (int, []byte) {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return http.StatusInternalServerError, []byte(err.Error())
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader).WithContext(
		context.WithValue(context.WithValue(ctx, internalUserKey, user), internalViaKey, via))
	req.RemoteAddr = via + ":0"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.internalRoutes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (s *Server) internalRoutes() http.Handler {
	s.internalOnce.Do(func() { s.internalMux = s.Routes() })
	return s.internalMux
}

func internalUser(ctx context.Context) *paneldb.User {
	u, _ := ctx.Value(internalUserKey).(*paneldb.User)
	return u
}

func internalVia(ctx context.Context) string {
	v, _ := ctx.Value(internalViaKey).(string)
	return v
}

type consoleCommandRequest struct {
	Command string `json:"command"`
}

const maxConsoleCommand = 512

// handleConsoleCommand types one line into the game's console (the stdin of PID 1), the same way a
// schedule's Command task does. The command is passed as an argument, never interpolated.
func (s *Server) handleConsoleCommand(w http.ResponseWriter, r *http.Request) {
	var req consoleCommandRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" || len(cmd) > maxConsoleCommand || strings.ContainsFunc(cmd, unicode.IsControl) {
		writeError(w, http.StatusUnprocessableEntity, "command must be a single line of 1 to 512 characters")
		return
	}
	ns, name := r.PathValue("namespace"), r.PathValue("name")
	var pod corev1.Pod
	if err := s.Client.Get(r.Context(), types.NamespacedName{Namespace: ns, Name: name}, &pod); err != nil || !gameContainerRunning(&pod) {
		writeError(w, http.StatusConflict, "the server is not running")
		return
	}
	exec := s.execFn
	if exec == nil {
		exec = s.execInPod
	}
	if _, err := exec(r.Context(), ns, name, gameContainerName, []string{"sh", "-c", `printf '%s\n' "$1" > /proc/1/fd/0`, "sh", cmd}); err != nil {
		writeError(w, http.StatusBadGateway, "could not reach the game console: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func gameContainerRunning(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name == gameContainerName {
			return st.State.Running != nil
		}
	}
	return false
}
