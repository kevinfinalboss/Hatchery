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
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/kevinfinalboss/Hatchery/pkg/authtoken"
)

const testServerUUID = "test-server-uuid"

// startTestServer boots a Server against t.TempDir(), returns it already
// listening, and arranges for it to stop when the test ends.
func startTestServer(t *testing.T) (*Server, []byte, string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	secret := []byte("test-hmac-secret")
	root := t.TempDir()

	srv := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		Root:       root,
		ServerUUID: testServerUUID,
		HMACKey:    secret,
		HostKey:    hostKey,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return srv, secret, root
}

// dialSFTP opens an authenticated SFTP client against srv using password as
// the SSH password (the way every real client here authenticates: the
// session token *is* the password).
func dialSFTP(t *testing.T, srv *Server, password string) *sftp.Client {
	t.Helper()

	sshConn, err := ssh.Dial("tcp", srv.Addr().String(), &ssh.ClientConfig{
		User:            "sftp",
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { sshConn.Close() })

	client, err := sftp.NewClient(sshConn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestAuthRejectsInvalidToken(t *testing.T) {
	srv, _, _ := startTestServer(t)

	_, err := ssh.Dial("tcp", srv.Addr().String(), &ssh.ClientConfig{
		User:            "sftp",
		Auth:            []ssh.AuthMethod{ssh.Password("not-a-real-token")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected dial with an invalid token to fail")
	}
}

func TestAuthRejectsTokenForAnotherServer(t *testing.T) {
	srv, secret, _ := startTestServer(t)

	token, err := authtoken.Sign(secret, "some-other-server", authtoken.ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ssh.Dial("tcp", srv.Addr().String(), &ssh.ClientConfig{
		User:            "sftp",
		Auth:            []ssh.AuthMethod{ssh.Password(token)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected dial with a token minted for a different server to fail")
	}
}

func TestReadWriteListWithinRoot(t *testing.T) {
	srv, secret, root := startTestServer(t)

	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello from disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	token, err := authtoken.Sign(secret, testServerUUID, authtoken.ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := dialSFTP(t, srv, token)

	t.Run("read an existing file", func(t *testing.T) {
		f, err := client.Open("hello.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hello from disk" {
			t.Fatalf("unexpected contents: %q", data)
		}
	})

	t.Run("write a new file and see it land under root", func(t *testing.T) {
		f, err := client.Create("uploaded.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("uploaded via sftp")); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(root, "uploaded.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "uploaded via sftp" {
			t.Fatalf("unexpected contents on disk: %q", data)
		}
	})

	t.Run("list the root directory", func(t *testing.T) {
		entries, err := client.ReadDir(".")
		if err != nil {
			t.Fatal(err)
		}
		names := make(map[string]bool, len(entries))
		for _, e := range entries {
			names[e.Name()] = true
		}
		if !names["hello.txt"] {
			t.Fatalf("expected hello.txt in listing, got %v", names)
		}
	})

	t.Run("mkdir then remove", func(t *testing.T) {
		if err := client.Mkdir("subdir"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "subdir")); err != nil {
			t.Fatalf("expected subdir to exist on disk: %v", err)
		}
		if err := client.Remove("subdir"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "subdir")); !os.IsNotExist(err) {
			t.Fatalf("expected subdir to be gone, stat err = %v", err)
		}
	})
}

func TestCannotEscapeRoot(t *testing.T) {
	srv, secret, root := startTestServer(t)

	// A sibling of root, outside it, that a traversal attempt might try to
	// reach.
	outside := filepath.Join(filepath.Dir(root), "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("should not be reachable"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	token, err := authtoken.Sign(secret, testServerUUID, authtoken.ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := dialSFTP(t, srv, token)

	if _, err := client.Open("../outside-secret.txt"); err == nil {
		t.Fatal("expected opening a path outside root to fail")
	}
}

func TestRemoveMissingPathFails(t *testing.T) {
	srv, secret, root := startTestServer(t)
	token, err := authtoken.Sign(secret, testServerUUID, authtoken.ScopeSFTP, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client := dialSFTP(t, srv, token)

	if err := client.Remove("missing.txt"); err == nil {
		t.Fatal("removing a missing file must fail")
	}
	if err := client.RemoveDirectory("missing-dir"); err == nil {
		t.Fatal("removing a missing directory must fail")
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveDirectory("file.txt"); err == nil {
		t.Fatal("rmdir on a regular file must fail")
	}
	if err := os.MkdirAll(filepath.Join(root, "dir", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveDirectory("dir"); err != nil {
		t.Fatalf("recursive directory removal: %v", err)
	}
	if err := client.Remove("file.txt"); err != nil {
		t.Fatalf("removing an existing file: %v", err)
	}
}
