package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func runAsEgg(uid *int64) *gameserversv1alpha1.Egg {
	return &gameserversv1alpha1.Egg{Spec: gameserversv1alpha1.EggSpec{
		Images:       []gameserversv1alpha1.EggImage{{Name: "default", Image: "example.com/g:1"}},
		StartCommand: "run",
		Install:      &gameserversv1alpha1.EggInstall{Script: "echo install"},
		Configure:    &gameserversv1alpha1.EggConfigure{Script: "echo configure"},
		RunAsUser:    uid,
	}}
}

func runAsServer() *gameserversv1alpha1.GameServer {
	return &gameserversv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: "default"},
		Spec:       gameserversv1alpha1.GameServerSpec{Storage: gameserversv1alpha1.GameServerStorage{Size: "1Gi"}, InstallRevision: 3},
	}
}

func TestBuildPodRunAsUser(t *testing.T) {
	pod, err := buildPod(runAsServer(), runAsEgg(ptr.To[int64](1000)), testSFTPAgentImage)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "install,fix-owner,configure" {
		t.Fatalf("init containers = %v", names)
	}
	install, fix, configure := pod.Spec.InitContainers[0], pod.Spec.InitContainers[1], pod.Spec.InitContainers[2]
	if install.SecurityContext.RunAsUser != nil || fix.SecurityContext.RunAsUser != nil {
		t.Error("install and fix-owner must run as the image's user (root) to chown")
	}
	for _, c := range []struct {
		name string
		uid  *int64
		gid  *int64
	}{{"configure", configure.SecurityContext.RunAsUser, configure.SecurityContext.RunAsGroup},
		{"server", pod.Spec.Containers[0].SecurityContext.RunAsUser, pod.Spec.Containers[0].SecurityContext.RunAsGroup}} {
		if c.uid == nil || *c.uid != 1000 || c.gid == nil || *c.gid != 1000 {
			t.Errorf("%s must run as 1000:1000, got %v:%v", c.name, c.uid, c.gid)
		}
	}
	script := strings.Join(fix.Command, " ")
	if !strings.Contains(script, "chown -R") || !strings.Contains(script, `chown "${HATCHERY_OWNER}" /data/.hatchery-owner`) {
		t.Errorf("fix-owner command = %q", script)
	}
	env := map[string]string{}
	for _, e := range fix.Env {
		env[e.Name] = e.Value
	}
	if env["HATCHERY_OWNER"] != "1000:1000" || env["HATCHERY_INSTALL_REVISION"] != "3" {
		t.Errorf("fix-owner env = %v", env)
	}
}

func TestBuildPodWithoutRunAsUserKeepsImageUser(t *testing.T) {
	pod, err := buildPod(runAsServer(), runAsEgg(nil), testSFTPAgentImage)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range pod.Spec.InitContainers {
		if c.Name == "fix-owner" {
			t.Fatal("fix-owner only exists when the Egg sets runAsUser")
		}
	}
	if pod.Spec.Containers[0].SecurityContext.RunAsUser != nil {
		t.Fatal("server must keep the image's user")
	}
}

// The install step gets the server's resources: building (Spigot's BuildTools) or running a
// loader installer (NeoForge) needs more than the namespace LimitRange default, and it costs no
// extra quota since init containers and app containers never run at the same time.
func TestBuildPodInstallGetsServerResources(t *testing.T) {
	gs := runAsServer()
	gs.Spec.Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi"), corev1.ResourceCPU: resource.MustParse("2")},
	}
	pod, err := buildPod(gs, runAsEgg(nil), testSFTPAgentImage)
	if err != nil {
		t.Fatal(err)
	}
	install := pod.Spec.InitContainers[0]
	if install.Name != "install" || install.Resources.Limits.Memory().String() != "4Gi" || install.Resources.Limits.Cpu().String() != "2" {
		t.Fatalf("install resources = %+v", install.Resources)
	}
}
