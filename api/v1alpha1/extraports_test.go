package v1alpha1

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func extraEgg() *Egg {
	return &Egg{Spec: EggSpec{Ports: []EggPort{{Name: "game", ContainerPort: 25565}}}}
}

func TestEffectivePorts(t *testing.T) {
	gs := &GameServer{Spec: GameServerSpec{ExtraPorts: []GameServerExtraPort{
		{Name: "dynmap", ContainerPort: 8123, Protocol: corev1.ProtocolTCP},
		{Name: "voice", ContainerPort: 24454, Protocol: corev1.ProtocolUDP},
		{Name: "clash", ContainerPort: 25565, Protocol: corev1.ProtocolTCP}, // collides with the Egg's port
		{Name: "game", ContainerPort: 9000, Protocol: corev1.ProtocolTCP},   // collides with the Egg's name
	}}}
	got := EffectivePorts(extraEgg(), gs)
	var names []string
	for _, p := range got {
		names = append(names, p.Name+"/"+string(p.Protocol))
	}
	if strings.Join(names, ",") != "game/TCP,dynmap/TCP,voice/UDP" {
		t.Fatalf("EffectivePorts = %v", names)
	}
}

func TestValidateExtraPorts(t *testing.T) {
	ok := []GameServerExtraPort{
		{Name: "voice", ContainerPort: 24454, Protocol: corev1.ProtocolUDP},
		{Name: "map", ContainerPort: 24454, Protocol: corev1.ProtocolTCP},
	}
	if msgs := ValidateExtraPorts(extraEgg(), ok); len(msgs) != 0 {
		t.Fatalf("valid extras rejected: %v", msgs)
	}
	p := func(name string, port int32, proto corev1.Protocol) GameServerExtraPort {
		return GameServerExtraPort{Name: name, ContainerPort: port, Protocol: proto}
	}
	six := []GameServerExtraPort{}
	for i := range 6 {
		six = append(six, p("p"+string(rune('a'+i)), int32(9000+i), corev1.ProtocolTCP))
	}
	bad := map[string][]GameServerExtraPort{
		"too many":        six,
		"uppercase name":  {p("Map", 8123, corev1.ProtocolTCP)},
		"underscore":      {p("a_b", 8123, corev1.ProtocolTCP)},
		"leading dash":    {p("-x", 8123, corev1.ProtocolTCP)},
		"16 chars":        {p("abcdefghijklmnop", 8123, corev1.ProtocolTCP)},
		"repeated name":   {p("map", 8123, corev1.ProtocolTCP), p("map", 8124, corev1.ProtocolTCP)},
		"egg port name":   {p("game", 8123, corev1.ProtocolTCP)},
		"port too low":    {p("web", 80, corev1.ProtocolTCP)},
		"port too high":   {p("web", 70000, corev1.ProtocolTCP)},
		"bad protocol":    {p("web", 8123, corev1.ProtocolSCTP)},
		"egg port number": {p("web", 25565, corev1.ProtocolTCP)},
		"sftp port":       {p("web", 2022, corev1.ProtocolTCP)},
		"repeated port":   {p("a", 8123, corev1.ProtocolTCP), p("b", 8123, corev1.ProtocolTCP)},
	}
	for name, extras := range bad {
		if msgs := ValidateExtraPorts(extraEgg(), extras); len(msgs) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	// 2022/UDP is fine: the sftp-agent only listens on TCP.
	if msgs := ValidateExtraPorts(extraEgg(), []GameServerExtraPort{p("x", 2022, corev1.ProtocolUDP)}); len(msgs) != 0 {
		t.Errorf("2022/UDP rejected: %v", msgs)
	}
}
