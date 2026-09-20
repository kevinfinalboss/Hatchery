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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
)

// startTestSFTPAgent boots a real sftp-agent SSH/SFTP server rooted at a
// fresh temp dir and returns its listening address. It stops when the test
// ends. Mirrors internal/sftpagent/server_test.go's startTestServer, which
// can't be imported directly since it's unexported in another package.
func startTestSFTPAgent(t *testing.T, hmacKey []byte, serverUUID string) net.Addr {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	srv := sftpagent.NewServer(sftpagent.Config{
		ListenAddr: "127.0.0.1:0",
		Root:       t.TempDir(),
		ServerUUID: serverUUID,
		HMACKey:    hmacKey,
		HostKey:    hostKey,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return srv.Addr()
}

// newFileManagerTestServer wires up a *Server, a GameServer+Secret(+PVC for
// the Stopped case), and a real in-process sftp-agent, with resolveSFTPAddr
// pointed at it — the shared setup every files_test.go test in Tasks 3-6
// also uses. Returns the server, the GameServer, and an admin session token.
func newFileManagerTestServer(t *testing.T, state gameserversv1alpha1.GameServerState) (*Server, *gameserversv1alpha1.GameServer, string) {
	t.Helper()
	gs, secret := newTestGameServerWithSecret("gs-files-"+string(state), state)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: gs.Name, Namespace: testOrgNS()},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	srv := newTestServer(t, gs, secret, pvc)
	addr := startTestSFTPAgent(t, secret.Data["hmac-key"], string(gs.UID))
	srv.resolveSFTPAddr = func(_, _ string) string { return addr.String() }
	token := adminToken(t, srv)
	return srv, gs, token
}

func TestOpenFileSFTPClientSidecarMode(t *testing.T) {
	srv, gs, _ := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateRunning)

	conn, err := srv.openFileSFTPClient(context.Background(), gs)
	if err != nil {
		t.Fatalf("openFileSFTPClient: %v", err)
	}
	defer conn.Close()

	f, err := conn.Create("hello.txt")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()

	entries, err := conn.ReadDir("/")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "hello.txt" {
		t.Fatalf("expected one file hello.txt, got %+v", entries)
	}
}

func TestOpenFileSFTPClientMaintenanceModeCreatesPod(t *testing.T) {
	srv, gs, _ := newFileManagerTestServer(t, gameserversv1alpha1.GameServerStateStopped)

	conn, err := srv.openFileSFTPClient(context.Background(), gs)
	if err != nil {
		t.Fatalf("openFileSFTPClient: %v", err)
	}
	defer conn.Close()

	var pod corev1.Pod
	if err := srv.Client.Get(context.Background(), client.ObjectKey{Namespace: testOrgNS(), Name: maintenancePodName(gs.Name)}, &pod); err != nil {
		t.Fatalf("expected maintenance pod to exist: %v", err)
	}
}
