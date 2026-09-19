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

// Command sftp-agent is the per-GameServer SFTP endpoint: a sidecar when the
// server is Running, or a standalone on-demand Pod when it's Stopped (see
// AGENTS.md's SFTP section for why Kubernetes needs this and exec/logs
// don't). It has no user database of its own — every session is authorized
// by a short-lived token the Panel API mints, verified locally against a
// secret shared out of band (the GameServerController-created Secret this
// binary is handed via --hmac-key-file).
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
)

func main() {
	var listen, root, serverUUID, hmacKeyFile string
	flag.StringVar(&listen, "listen", ":2022", "Address the SFTP server listens on.")
	flag.StringVar(&root, "root", "/data", "Directory SFTP sessions are confined to.")
	flag.StringVar(&serverUUID, "server-uuid", "", "GameServer UID that session tokens must be issued for.")
	flag.StringVar(&hmacKeyFile, "hmac-key-file", os.Getenv(sftpagent.HMACKeyFileEnv),
		"Path to the file holding the shared HMAC secret. Defaults to $"+sftpagent.HMACKeyFileEnv+".")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if serverUUID == "" {
		logger.Error("--server-uuid is required")
		os.Exit(1)
	}
	if hmacKeyFile == "" {
		logger.Error("--hmac-key-file (or $" + sftpagent.HMACKeyFileEnv + ") is required")
		os.Exit(1)
	}
	key, err := os.ReadFile(hmacKeyFile)
	if err != nil {
		logger.Error("reading hmac key file", "path", hmacKeyFile, "error", err)
		os.Exit(1)
	}

	// A fresh host key every start is fine for now: clients authenticate the
	// *server process* with the session token (via password auth), not the
	// other way around, and nothing here persists a known_hosts-style pin
	// yet. Revisit if/when that changes.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		logger.Error("generating host key", "error", err)
		os.Exit(1)
	}
	hostKey, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		logger.Error("building host key signer", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := sftpagent.NewServer(sftpagent.Config{
		ListenAddr: listen,
		Root:       root,
		ServerUUID: serverUUID,
		HMACKey:    key,
		HostKey:    hostKey,
		Logger:     logger,
	})
	if err := srv.Serve(ctx); err != nil {
		logger.Error("sftp-agent stopped", "error", err)
		os.Exit(1)
	}
}
