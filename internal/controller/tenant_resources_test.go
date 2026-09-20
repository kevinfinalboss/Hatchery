package controller

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

func TestTenantQuotaHard(t *testing.T) {
	hard := tenantQuotaHard(gameserversv1alpha1.TenantQuota{
		CPU:            resource.MustParse("4"),
		Memory:         resource.MustParse("8Gi"),
		Storage:        resource.MustParse("50Gi"),
		MaxGameServers: 3,
	})

	want := map[corev1.ResourceName]string{
		corev1.ResourceRequestsCPU:                  "4",
		corev1.ResourceRequestsMemory:               "8Gi",
		corev1.ResourceRequestsStorage:              "50Gi",
		"count/gameservers.gameservers.hatchery.io": "3",
	}
	for name, v := range want {
		got, ok := hard[name]
		if !ok {
			t.Fatalf("quota missing %q", name)
		}
		if got.Cmp(resource.MustParse(v)) != 0 {
			t.Errorf("%q = %s, want %s", name, got.String(), v)
		}
	}
}

func TestTenantLimitRangeSetsContainerDefaults(t *testing.T) {
	spec := tenantLimitRangeSpec()
	if len(spec.Limits) != 1 || spec.Limits[0].Type != corev1.LimitTypeContainer {
		t.Fatalf("expected exactly one Container limit, got %+v", spec.Limits)
	}
	l := spec.Limits[0]
	if l.DefaultRequest.Cpu().IsZero() || l.DefaultRequest.Memory().IsZero() {
		t.Error("DefaultRequest must set cpu and memory so quota accounting works for containers that declare no requests (the sftp-agent sidecar)")
	}
	if l.Default.Cpu().IsZero() || l.Default.Memory().IsZero() {
		t.Error("Default (limits) must set cpu and memory")
	}
}

func policyByName(t *testing.T, ps []*networkingv1.NetworkPolicy, name string) *networkingv1.NetworkPolicy {
	t.Helper()
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("policy %q not found in %d policies", name, len(ps))
	return nil
}

func TestTenantNetworkPolicies(t *testing.T) {
	ps := tenantNetworkPolicies("hatchery-acme", "hatchery-panel", []string{"203.0.113.0/24"})

	deny := policyByName(t, ps, "default-deny")
	if len(deny.Spec.PodSelector.MatchLabels) != 0 || len(deny.Spec.PodSelector.MatchExpressions) != 0 {
		t.Error("default-deny must select every pod (empty selector)")
	}
	if len(deny.Spec.PolicyTypes) != 2 || len(deny.Spec.Ingress) != 0 || len(deny.Spec.Egress) != 0 {
		t.Errorf("default-deny must declare both policy types and no allow rules, got %+v", deny.Spec)
	}

	inet := policyByName(t, ps, "allow-internet-egress")
	cidr := inet.Spec.Egress[0].To[0].IPBlock
	if cidr.CIDR != "0.0.0.0/0" {
		t.Errorf("internet egress CIDR = %q", cidr.CIDR)
	}
	for _, must := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10", "203.0.113.0/24"} {
		if !slices.Contains(cidr.Except, must) {
			t.Errorf("internet egress must exclude %s, except=%v", must, cidr.Except)
		}
	}

	sftp := policyByName(t, ps, "allow-panel-sftp")
	from := sftp.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels
	if from["kubernetes.io/metadata.name"] != "hatchery-panel" {
		t.Errorf("sftp ingress must come only from the panel namespace, got %v", from)
	}
	if got := sftp.Spec.Ingress[0].Ports[0].Port.IntValue(); got != 2022 {
		t.Errorf("sftp ingress port = %d, want 2022", got)
	}
}

func TestTenantNetworkPoliciesWithoutPanelHaveNoSFTPRule(t *testing.T) {
	for _, p := range tenantNetworkPolicies("hatchery-acme", "", nil) {
		if p.Name == "allow-panel-sftp" {
			t.Fatal("no panel namespace configured, so no allow-panel-sftp policy should exist")
		}
	}
}

func TestGameIngressPolicySpecOpensOnlyTheEggPorts(t *testing.T) {
	egg := &gameserversv1alpha1.Egg{Spec: gameserversv1alpha1.EggSpec{Ports: []gameserversv1alpha1.EggPort{
		{Name: "game", ContainerPort: 25565},
		{Name: "query", ContainerPort: 19132, Protocol: corev1.ProtocolUDP},
	}}}
	spec := gameIngressPolicySpec("mc", egg)

	if spec.PodSelector.MatchLabels[gameserversv1alpha1.LabelGameServer] != "mc" {
		t.Errorf("policy must select only this GameServer's pods, got %v", spec.PodSelector.MatchLabels)
	}
	if len(spec.Ingress) != 1 || len(spec.Ingress[0].From) != 0 {
		t.Fatalf("expect one ingress rule with no 'from' (players connect from anywhere), got %+v", spec.Ingress)
	}
	ports := spec.Ingress[0].Ports
	if len(ports) != 2 || ports[0].Port.IntValue() != 25565 || *ports[0].Protocol != corev1.ProtocolTCP ||
		ports[1].Port.IntValue() != 19132 || *ports[1].Protocol != corev1.ProtocolUDP {
		t.Errorf("unexpected ports %+v", ports)
	}
}

func TestGameIngressPolicySpecWithNoPortsAllowsNothing(t *testing.T) {
	// An ingress rule with an empty ports list means "all ports" in
	// Kubernetes, so an Egg with no ports must produce no rule at all.
	spec := gameIngressPolicySpec("mc", &gameserversv1alpha1.Egg{})
	if len(spec.Ingress) != 0 {
		t.Fatalf("an Egg with no ports must not open anything, got %+v", spec.Ingress)
	}
}
