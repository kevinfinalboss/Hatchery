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
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
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

type copyRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	var req copyRequest
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

	info, err := conn.Stat(req.From)
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	if !info.IsDir() {
		if err := copyOneFile(conn, req.From, req.To); err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		return
	}

	walker := conn.Walk(req.From)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		rel := strings.TrimPrefix(walker.Path(), req.From)
		dest := path.Join(req.To, rel)
		if walker.Stat().IsDir() {
			if err := conn.MkdirAll(dest); err != nil {
				writeError(w, statusForSFTP(err), err.Error())
				return
			}
			continue
		}
		if err := copyOneFile(conn, walker.Path(), dest); err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

// copyOneFile streams src's content into dest over the same SFTP connection
// — the protocol has no native "copy" verb, so this is a plain read+write.
func copyOneFile(conn *sftpConn, src, dest string) error {
	srcFile, err := conn.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	destFile, err := conn.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()
	_, err = io.Copy(destFile, srcFile)
	return err
}

const maxUploadFormMemory = 100 << 20 // 100MB

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxUploadFormMemory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field: "+err.Error())
		return
	}
	defer file.Close()

	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	dest, err := conn.Create(path.Join(filePathParam(r), header.Filename))
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDownloadFiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	// Repeated `paths=` params, not one comma-joined value: Query().Get()
	// percent-decodes before we could split, so an escaped comma inside a
	// filename would un-escape into a real delimiter and corrupt the parse.
	paths := r.URL.Query()["paths"]
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, "paths is required")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	if len(paths) == 1 {
		info, err := conn.Stat(paths[0])
		if err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		if !info.IsDir() {
			f, err := conn.Open(paths[0])
			if err != nil {
				writeError(w, statusForSFTP(err), err.Error())
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(paths[0])+`"`)
			_, _ = io.Copy(w, f)
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="download.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()
	_ = writeZipEntries(zw, conn, paths) // headers are already sent; nothing left to do on error but stop writing.
}

// writeZipEntries adds every regular file under each of paths (recursively,
// for a directory) to zw, named relative to that path's own parent — so
// zipping "/configs" produces zip entries rooted at "configs/...". Shared by
// handleDownloadFiles above and handleCompressFiles (Task 6) so the
// tree-walking logic exists exactly once.
func writeZipEntries(zw *zip.Writer, conn *sftpConn, paths []string) error {
	for _, p := range paths {
		info, err := conn.Stat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := addFileToZip(zw, conn, p, path.Base(p)); err != nil {
				return err
			}
			continue
		}
		base := path.Base(p)
		walker := conn.Walk(p)
		for walker.Step() {
			if err := walker.Err(); err != nil {
				return err
			}
			if walker.Stat().IsDir() {
				continue
			}
			rel := path.Join(base, strings.TrimPrefix(walker.Path(), p))
			if err := addFileToZip(zw, conn, walker.Path(), rel); err != nil {
				return err
			}
		}
	}
	return nil
}

func addFileToZip(zw *zip.Writer, conn *sftpConn, srcPath, zipName string) error {
	f, err := conn.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	entry, err := zw.Create(zipName)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, f)
	return err
}

type compressRequest struct {
	Paths []string `json:"paths"`
	Dest  string   `json:"dest"`
}

func (s *Server) handleCompressFiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	var req compressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Paths) == 0 || req.Dest == "" {
		writeError(w, http.StatusBadRequest, "paths and dest are required")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	destFile, err := conn.Create(req.Dest)
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	defer destFile.Close()

	zw := zip.NewWriter(destFile)
	if err := writeZipEntries(zw, conn, req.Paths); err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	if err := zw.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

type decompressRequest struct {
	Path string `json:"path"`
	Dest string `json:"dest"`
}

func (s *Server) handleDecompressFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireGameServerAccess(w, r) {
		return
	}
	gs, ok := s.targetGameServer(w, r)
	if !ok {
		return
	}
	var req decompressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" || req.Dest == "" {
		writeError(w, http.StatusBadRequest, "path and dest are required")
		return
	}
	conn, err := s.openFileSFTPClient(r.Context(), gs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer conn.Close()

	srcFile, err := conn.Open(req.Path)
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}
	defer srcFile.Close()
	stat, err := srcFile.Stat()
	if err != nil {
		writeError(w, statusForSFTP(err), err.Error())
		return
	}

	zr, err := zip.NewReader(srcFile, stat.Size())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "not a valid zip file: "+err.Error())
		return
	}
	for _, f := range zr.File {
		destPath := path.Join(req.Dest, f.Name)
		if f.FileInfo().IsDir() {
			if err := conn.MkdirAll(destPath); err != nil {
				writeError(w, statusForSFTP(err), err.Error())
				return
			}
			continue
		}
		if err := conn.MkdirAll(path.Dir(destPath)); err != nil {
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		rc, err := f.Open()
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		destFile, err := conn.Create(destPath)
		if err != nil {
			rc.Close()
			writeError(w, statusForSFTP(err), err.Error())
			return
		}
		_, copyErr := io.Copy(destFile, rc)
		rc.Close()
		destFile.Close()
		if copyErr != nil {
			writeError(w, http.StatusBadGateway, copyErr.Error())
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}
