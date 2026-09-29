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

// The file manager (files.go, filesftp.go) is an SFTP client of the server's sftp-agent.
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

// fileOperationTokenTTL: the token lives only for one request's connection.
const fileOperationTokenTTL = 30 * time.Second

// sftpConn closes the SFTP client and its SSH connection together.
type sftpConn struct {
	*sftp.Client
	ssh *ssh.Client
}

func (c *sftpConn) Close() error {
	_ = c.Client.Close()
	return c.ssh.Close()
}

// openFileSFTPClient connects to the server's sftp-agent for one request. Callers must Close it.
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

// sftpAddr is the sftp-agent's Service address; tests override resolveSFTPAddr.
func (s *Server) sftpAddr(namespace, name string) string {
	if s.resolveSFTPAddr != nil {
		return s.resolveSFTPAddr(namespace, name)
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", name, namespace, sftpagent.Port)
}
