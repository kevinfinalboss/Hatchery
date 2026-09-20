package controller

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
)

const (
	panelSFTPPolicyName  = "allow-panel-sftp"
	tenantQuotaName      = "tenant-quota"
	tenantLimitRangeName = "tenant-defaults"
)

// gameServerCountResource is the object-count quota key for GameServers:
// "count/<resource>.<group>" is how Kubernetes quotas arbitrary CRDs.
const gameServerCountResource corev1.ResourceName = "count/gameservers.gameservers.hatchery.io"

// tenantQuotaHard translates a Tenant's budget into ResourceQuota.spec.hard.
func tenantQuotaHard(q gameserversv1alpha1.TenantQuota) corev1.ResourceList {
	return corev1.ResourceList{
		corev1.ResourceRequestsCPU:     q.CPU,
		corev1.ResourceRequestsMemory:  q.Memory,
		corev1.ResourceRequestsStorage: q.Storage,
		gameServerCountResource:        *resource.NewQuantity(int64(q.MaxGameServers), resource.DecimalSI),
	}
}

// tenantLimitRangeSpec gives every container that declares no resources
// defaults. A ResourceQuota on requests.* rejects any pod whose containers
// have no requests, and the sftp-agent sidecar declares none — without this
// LimitRange no GameServer pod could ever be admitted in a tenant namespace.
func tenantLimitRangeSpec() corev1.LimitRangeSpec {
	return corev1.LimitRangeSpec{
		Limits: []corev1.LimitRangeItem{{
			Type: corev1.LimitTypeContainer,
			DefaultRequest: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Default: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}},
	}
}

// defaultEgressExceptCIDRs are ranges tenant pods may never reach through the
// internet egress rule: RFC1918 private space (which covers the pod and
// service CIDRs of most clusters), link-local (cloud metadata at
// 169.254.169.254) and carrier-grade NAT space.
var defaultEgressExceptCIDRs = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10",
}

func tenantNetworkPolicies(ns, panelNamespace string, extraExcept []string) []*networkingv1.NetworkPolicy {
	udp, tcp := corev1.ProtocolUDP, corev1.ProtocolTCP
	dnsPort := intstr.FromInt32(53)
	sftpPort := intstr.FromInt32(sftpagent.Port)

	np := func(name string, spec networkingv1.NetworkPolicySpec) *networkingv1.NetworkPolicy {
		return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: spec}
	}
	all := metav1.LabelSelector{}

	except := append(append([]string{}, defaultEgressExceptCIDRs...), extraExcept...)

	policies := []*networkingv1.NetworkPolicy{
		np("default-deny", networkingv1.NetworkPolicySpec{
			PodSelector: all,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		}),
		np("allow-dns-egress", networkingv1.NetworkPolicySpec{
			PodSelector: all,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &udp, Port: &dnsPort},
					{Protocol: &tcp, Port: &dnsPort},
				},
			}},
		}),
		np("allow-internet-egress", networkingv1.NetworkPolicySpec{
			PodSelector: all,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0", Except: except},
				}},
			}},
		}),
		np("allow-same-namespace", networkingv1.NetworkPolicySpec{
			PodSelector: all,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
			}},
		}),
	}

	if panelNamespace != "" {
		policies = append(policies, np(panelSFTPPolicyName, networkingv1.NetworkPolicySpec{
			PodSelector: all,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": panelNamespace}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &sftpPort}},
			}},
		}))
	}
	return policies
}
