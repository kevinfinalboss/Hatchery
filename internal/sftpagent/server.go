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

package sftpagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/kevinfinalboss/Hatchery/pkg/authtoken"
)

// Config configures a Server.
type Config struct {
	// ListenAddr is the address the SSH server binds to, e.g. ":2022".
	ListenAddr string
	// Root is the directory SFTP sessions are confined to — os.Root (Go
	// 1.24+) enforces that confinement at the OS level, so there's no
	// hand-rolled ".." filtering to get wrong here.
	Root string
	// ServerUUID must match the server_uuid claim of every session token
	// presented as the SSH password.
	ServerUUID string
	// HMACKey is the shared secret used to verify session tokens.
	HMACKey []byte
	// HostKey signs the SSH server's identity.
	HostKey ssh.Signer
	Logger  *slog.Logger
}

// Server is a minimal SSH server that only speaks the sftp subsystem,
// authenticating each connection against a short-lived token minted by the
// Panel API (pkg/authtoken) instead of a real user database — there is none
// here, only "does this token prove the Panel API authorized an SFTP session
// for this exact GameServer right now". See AGENTS.md's SFTP section.
type Server struct {
	cfg   Config
	ready chan struct{}
	addr  net.Addr
}

func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{cfg: cfg, ready: make(chan struct{})}
}

// Addr blocks until Serve has bound its listener and returns the address it
// bound to — useful when ListenAddr ends in ":0" and the actual port is
// picked by the OS, as tests do.
func (s *Server) Addr() net.Addr {
	<-s.ready
	return s.addr
}

// Serve accepts connections on cfg.ListenAddr until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	sshConfig := &ssh.ServerConfig{PasswordCallback: s.authenticate}
	sshConfig.AddHostKey(s.cfg.HostKey)

	root, err := os.OpenRoot(s.cfg.Root)
	if err != nil {
		return fmt.Errorf("opening root %q: %w", s.cfg.Root, err)
	}
	defer root.Close()
	handlers := sftp.Handlers{
		FileGet:  &rootedHandler{root: root},
		FilePut:  &rootedHandler{root: root},
		FileCmd:  &rootedHandler{root: root},
		FileList: &rootedHandler{root: root},
	}

	listener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.ListenAddr, err)
	}
	defer listener.Close()
	s.addr = listener.Addr()
	close(s.ready)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	s.cfg.Logger.Info("sftp-agent listening", "addr", s.addr.String(), "root", s.cfg.Root)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(conn, sshConfig, handlers)
	}
}

func (s *Server) authenticate(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
	if _, err := authtoken.Verify(s.cfg.HMACKey, string(password), s.cfg.ServerUUID, authtoken.ScopeSFTP); err != nil {
		return nil, fmt.Errorf("session token rejected: %w", err)
	}
	return nil, nil
}

func (s *Server) handleConn(nConn net.Conn, config *ssh.ServerConfig, handlers sftp.Handlers) {
	defer nConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		s.cfg.Logger.Info("sftp-agent: rejected connection", "remote", nConn.RemoteAddr(), "error", err)
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sftp sessions are supported")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(channel, requests, handlers)
	}
}

func (s *Server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request, handlers sftp.Handlers) {
	defer channel.Close()

	for req := range requests {
		if req.Type != "subsystem" || string(req.Payload[4:]) != "sftp" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)

		server := sftp.NewRequestServer(channel, handlers)
		defer server.Close()
		if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
			s.cfg.Logger.Info("sftp-agent: session ended", "error", err)
		}
		return
	}
}

// rootedHandler implements sftp.Handlers against an os.Root, translating
// every SFTP path (always POSIX-absolute-style, e.g. "/foo/bar") into a path
// relative to the root. os.Root itself refuses anything that would escape
// the directory it was opened on, including via ".." or a symlink — this
// type only has to strip the leading "/", not re-derive that safety.
type rootedHandler struct {
	root *os.Root
}

func relPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "."
	}
	return p
}

func (h *rootedHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return h.root.Open(relPath(r.Filepath))
}

func (h *rootedHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	flags := os.O_WRONLY | os.O_CREATE
	if r.Pflags().Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return h.root.OpenFile(relPath(r.Filepath), flags, 0o644)
}

func (h *rootedHandler) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Mkdir":
		return h.root.Mkdir(relPath(r.Filepath), 0o755)
	case "Rmdir":
		p := relPath(r.Filepath)
		info, err := h.root.Stat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return &os.PathError{Op: "rmdir", Path: r.Filepath, Err: syscall.ENOTDIR}
		}
		return h.root.RemoveAll(p)
	case "Remove":
		return h.root.Remove(relPath(r.Filepath))
	case "Rename":
		return h.root.Rename(relPath(r.Filepath), relPath(r.Target))
	case "Setstat":
		// Permission/mtime changes aren't modeled yet; accept the request
		// as a no-op rather than failing transfers that set them.
		return nil
	default:
		return fmt.Errorf("unsupported sftp command %q", r.Method)
	}
}

func (h *rootedHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		dir, err := h.root.Open(relPath(r.Filepath))
		if err != nil {
			return nil, err
		}
		defer dir.Close()
		entries, err := dir.ReadDir(-1)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(entries))
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil {
				infos = append(infos, info)
			}
		}
		return fileInfoLister(infos), nil
	case "Stat":
		info, err := h.root.Stat(relPath(r.Filepath))
		if err != nil {
			return nil, err
		}
		return fileInfoLister{info}, nil
	default:
		return nil, fmt.Errorf("unsupported sftp list method %q", r.Method)
	}
}

// fileInfoLister adapts a []os.FileInfo to sftp.ListerAt, the interface
// pkg/sftp wants back from Filelist.
type fileInfoLister []os.FileInfo

func (l fileInfoLister) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[offset:])
	if n < len(dst) {
		return n, io.EOF
	}
	return n, nil
}
