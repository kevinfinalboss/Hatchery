package v1alpha1

import (
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"

	"github.com/kevinfinalboss/Hatchery/internal/sftpagent"
)

// MaxExtraPorts caps GameServer.spec.extraPorts.
const MaxExtraPorts = 5

// extraPortName is IANA_SVC_NAME: the name becomes a Service port name.
var extraPortName = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,13}[a-z0-9])?$`)

type portKey struct {
	port     int32
	protocol corev1.Protocol
}

func eggPortProtocol(p EggPort) corev1.Protocol {
	if p.Protocol == "" {
		return corev1.ProtocolTCP
	}
	return p.Protocol
}

// EffectivePorts is every port the server listens on: the Egg's first, then the server's extra
// ports. It is the only place Service, NetworkPolicy, Pod and public exposure get their ports from.
// An extra port that collides with one of the Egg's (by name, or by number and protocol) is dropped:
// the webhook refuses that, but the Egg can change after the server was admitted.
func EffectivePorts(egg *Egg, gs *GameServer) []EggPort {
	out := make([]EggPort, 0, len(egg.Spec.Ports)+len(gs.Spec.ExtraPorts))
	names := map[string]bool{}
	numbers := map[portKey]bool{}
	for _, p := range egg.Spec.Ports {
		p.Protocol = eggPortProtocol(p)
		out = append(out, p)
		names[p.Name] = true
		numbers[portKey{p.ContainerPort, p.Protocol}] = true
	}
	for _, x := range gs.Spec.ExtraPorts {
		k := portKey{x.ContainerPort, x.Protocol}
		if names[x.Name] || numbers[k] {
			continue
		}
		names[x.Name] = true
		numbers[k] = true
		out = append(out, EggPort{Name: x.Name, ContainerPort: x.ContainerPort, Protocol: x.Protocol})
	}
	return out
}

// ValidateExtraPorts checks a server's extra ports against its Egg. The webhook (authority, also for
// kubectl) and the Panel (422) both use it.
func ValidateExtraPorts(egg *Egg, extras []GameServerExtraPort) []string {
	var msgs []string
	if len(extras) > MaxExtraPorts {
		msgs = append(msgs, fmt.Sprintf("at most %d extra ports", MaxExtraPorts))
	}
	names := map[string]string{}
	numbers := map[portKey]string{{sftpagent.Port, corev1.ProtocolTCP}: "the SFTP port"}
	for _, p := range egg.Spec.Ports {
		names[p.Name] = "a port of the Egg"
		numbers[portKey{p.ContainerPort, eggPortProtocol(p)}] = fmt.Sprintf("the Egg's port %q", p.Name)
	}
	for _, x := range extras {
		if !extraPortName.MatchString(x.Name) {
			msgs = append(msgs, fmt.Sprintf("%q: name must be lowercase letters, digits and '-', start with a letter and have at most 15 characters", x.Name))
		}
		if x.ContainerPort < 1024 || x.ContainerPort > 65535 {
			msgs = append(msgs, fmt.Sprintf("%q: port must be between 1024 and 65535", x.Name))
		}
		if x.Protocol != corev1.ProtocolTCP && x.Protocol != corev1.ProtocolUDP {
			msgs = append(msgs, fmt.Sprintf("%q: protocol must be TCP or UDP", x.Name))
		}
		if owner, taken := names[x.Name]; taken {
			msgs = append(msgs, fmt.Sprintf("%q: name already used by %s", x.Name, owner))
		}
		names[x.Name] = "another extra port"
		k := portKey{x.ContainerPort, x.Protocol}
		if owner, taken := numbers[k]; taken {
			msgs = append(msgs, fmt.Sprintf("%q: %d/%s is already used by %s", x.Name, x.ContainerPort, x.Protocol, owner))
		}
		numbers[k] = fmt.Sprintf("extra port %q", x.Name)
	}
	return msgs
}
