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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

// fileEntry is one row of a directory listing.
type fileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	IsDir   bool   `json:"isDir"`
	ModTime string `json:"modTime"`
}

// statusForSFTP maps an error from an SFTP operation to the HTTP status the
// Panel API should answer with. pkg/sftp normalizes SSH_FX_STATUS codes to
// the stdlib os.Err* sentinels, so errors.Is works the same way statusFor
// works against Kubernetes API errors.
func statusForSFTP(err error) int {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, os.ErrPermission):
		return http.StatusForbidden
	case errors.Is(err, os.ErrExist):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// targetGameServer loads the GameServer named by the request's
// {namespace}/{name} path values, or writes the error response itself and
// reports false.
func (s *Server) targetGameServer(w http.ResponseWriter, r *http.Request) (*gameserversv1alpha1.GameServer, bool) {
	var gs gameserversv1alpha1.GameServer
	if err := s.Client.Get(r.Context(), gameServerKey(r), &gs); err != nil {
		writeError(w, statusFor(err), err.Error())
		return nil, false
	}
	return &gs, true
}

// filePathParam reads the "path" query parameter every file-manager GET
// endpoint takes, defaulting to the root.
func filePathParam(r *http.Request) string {
	p := r.URL.Query().Get("path")
	if p == "" {
		return "/"
	}
	return p
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	infos, err := conn.ReadDir(filePathParam(r))
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}

	entries := make([]fileEntry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, fileEntry{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    info.Mode().String(),
			IsDir:   info.IsDir(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleGetFileContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	f, err := conn.Open(filePathParam(r))
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, f)
}

func (s *Server) handlePutFileContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	f, err := conn.Create(filePathParam(r))
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	defer f.Close()

	if _, err := io.Copy(f, r.Body); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type mkdirRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	var req mkdirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	if err := conn.MkdirAll(req.Path); err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

type renameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.From == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	if err := conn.Rename(req.From, req.To); err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deleteFilesRequest struct {
	Paths []string `json:"paths"`
}

func (s *Server) handleDeleteFiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	var req deleteFilesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "paths is required")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	for _, p := range req.Paths {
		info, err := conn.Stat(p)
		if err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		if info.IsDir() {
			err = conn.RemoveDirectory(p) // server-side Rmdir is recursive — see internal/sftpagent/server.go's Filecmd
		} else {
			err = conn.Remove(p)
		}
		if err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
