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

// Package panelapi's file manager (files.go, filesftp.go) turns panel-api
// into an SFTP client of the existing sftp-agent, the same way Pterodactyl's
// Panel calls Wings' HTTP file API instead of tunneling SFTP to the browser
// — see docs/superpowers/specs/2026-09-19-file-manager-design.md.
package panelapi

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
	"github.com/kevinfinalboss/Hatchery/pkg/authtoken"
)

// fileOperationTokenTTL is how long the token panel-api mints for itself to
// authenticate one file-manager SFTP connection lives. It's minted, used to
// dial, and discarded within the same HTTP request, so seconds is plenty —
// contrast with sftpSessionTTL in sftpsession.go, which is handed to an
// external client that needs time to actually connect.
const fileOperationTokenTTL = 30 * time.Second

// sftpConn bundles an *sftp.Client with the *ssh.Client it rides on top of:
// sftp.Client.Close doesn't close the underlying SSH connection, and every
// file-manager request dials its own fresh one (see the "no connection
// pool" global constraint), so both need to go away together.
type sftpConn struct {
	*sftp.Client
	ssh *ssh.Client
}

func (c *sftpConn) Close() error {
	_ = c.Client.Close()
	return c.ssh.Close()
}

// openFileSFTPClient resolves gs's current sftp-agent (sidecar or an
// on-demand maintenance Pod — see resolveSFTPTarget), authenticates with a
// freshly-minted, seconds-lived token, and returns a connected sftpConn
// ready for one request's worth of file operations. Callers must Close it.
func (s *Server) openFileSFTPClient(ctx context.Context, gs *gameserversv1alpha1.GameServer) (*sftpConn, error) {
	if _, err := s.resolveSFTPTarget(ctx, gs); err != nil {
		return nil, err
	}

	var secret corev1.Secret
	secretKey := client.ObjectKey{Namespace: gs.Namespace, Name: sftpagent.SecretName(gs.Name)}
	if err := s.Client.Get(ctx, secretKey, &secret); err != nil {
		return nil, fmt.Errorf("sftp secret not ready yet: %w", err)
	}
	hmacKey, ok := secret.Data["hmac-key"]
	if !ok {
		return nil, fmt.Errorf("sftp secret is missing its hmac-key entry")
	}

	token, err := authtoken.Sign(hmacKey, string(gs.UID), authtoken.ScopeSFTP, fileOperationTokenTTL)
	if err != nil {
		return nil, err
	}

	sshConn, err := ssh.Dial("tcp", s.sftpAddr(gs.Namespace, gs.Name), &ssh.ClientConfig{
		User:            "sftp",
		Auth:            []ssh.AuthMethod{ssh.Password(token)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to sftp-agent: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshConn)
	if err != nil {
		_ = sshConn.Close()
		return nil, fmt.Errorf("starting sftp session: %w", err)
	}

	return &sftpConn{Client: sftpClient, ssh: sshConn}, nil
}

// sftpAddr returns the host:port to dial for a GameServer's sftp-agent.
// Defaults to the real in-cluster Service DNS name (sidecar and maintenance
// Pod sit behind the same Service — see AGENTS.md's SFTP section); tests set
// resolveSFTPAddr to point at an in-process test server instead.
func (s *Server) sftpAddr(namespace, name string) string {
	if s.resolveSFTPAddr != nil {
		return s.resolveSFTPAddr(namespace, name)
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", name, namespace, sftpagent.Port)
}
