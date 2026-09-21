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
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// handleLogs streams the server container's logs straight from the
// kube-apiserver's pods/log subresource. With ?follow=true it stays open and
// pushes new lines as they're written, flushed after every read so a client
// tailing this endpoint sees output in near real time rather than buffered in
// chunks.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	// The install and configure init containers are readable too, so the console can show
	// progress while a server installs; the sftp-agent sidecar is not exposed.
	container := r.URL.Query().Get("container")
	switch container {
	case "":
		container = "server"
	case "server", "install", "configure":
	default:
		writeError(w, http.StatusBadRequest, `container must be "server", "install" or "configure"`)
		return
	}

	opts := &corev1.PodLogOptions{
		Container:  container,
		Follow:     r.URL.Query().Get("follow") == "true",
		Timestamps: true,
	}
	if tail := r.URL.Query().Get("tailLines"); tail != "" {
		if n, err := strconv.ParseInt(tail, 10, 64); err == nil && n >= 0 {
			opts.TailLines = &n
		}
	}

	stream, err := s.Clientset.CoreV1().Pods(namespace).GetLogs(name, opts).Stream(r.Context())
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)

	buf := make([]byte, 4096)
	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			// Whether this is a clean io.EOF or a real error, the client
			// already has a partial response — writing an error body at this
			// point would just corrupt an already-started log stream, so
			// there's nothing left to do but stop.
			return
		}
	}
}
