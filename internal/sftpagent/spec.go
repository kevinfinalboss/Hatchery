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

// Package sftpagent has two halves that share one contract:
//
//   - Server (server.go), the SSH/SFTP server the sftp-agent binary
//     (cmd/sftp-agent) runs.
//   - Spec (this file), the Kubernetes container/volume shape both the
//     GameServer controller (sidecar, on a Running server) and the Panel API
//     (on-demand maintenance Pod, for a Stopped one) need to agree on so a
//     client can talk to either one the same way. See AGENTS.md's SFTP
//     section for why there are two places this container can run.
package sftpagent

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// DefaultDataVolumeName/DefaultDataMountPath are the canonical volume
	// name and mount path for a GameServer's data directory, shared by the
	// GameServer controller (the "server" container and its sidecar) and the
	// Panel API (the standalone maintenance Pod) so neither has to hardcode
	// its own copy that could drift from the other's.
	DefaultDataVolumeName = "data"
	DefaultDataMountPath  = "/data"

	// ContainerName is the name given to the sftp-agent container, whether
	// it's a sidecar or the sole container of a maintenance Pod.
	ContainerName = "sftp-agent"

	// Port is the SSH/SFTP listen port.
	Port = 2022

	// PortName is the port name used on both the container and the Service,
	// so kube-proxy/DNS-level tooling has a stable name to refer to.
	PortName = "sftp"

	secretVolumeName = "sftp-hmac"
	secretMountPath  = "/etc/hatchery/sftp"
	secretFileName   = "hmac-key"

	// HMACKeyFileEnv tells the sftp-agent binary where to read its signing
	// secret from; the volume mount below always puts it at the same path.
	HMACKeyFileEnv = "HATCHERY_SFTP_HMAC_KEY_FILE"
)

// SecretData builds the Secret data map for the per-GameServer HMAC key, so
// callers that create the Secret (the GameServer controller) don't need to
// know the exact key name Container's volume mount expects to find it under.
func SecretData(hmacKey []byte) map[string][]byte {
	return map[string][]byte{secretFileName: hmacKey}
}

// SecretName is the per-GameServer HMAC secret's name, derived deterministically
// from the GameServer's name so both the controller (which creates it) and the
// Panel API (which reads it to mint tokens) agree on where to find it without
// needing it recorded anywhere else.
func SecretName(gameServerName string) string {
	return gameServerName + "-sftp"
}

// Container builds the sftp-agent container spec. dataVolumeName/dataMountPath
// identify the volume holding the GameServer's files — the caller owns
// actually declaring that volume (it's the same data PVC the "server"
// container uses when this is a sidecar, or the whole point of the Pod when
// it's a standalone maintenance Pod).
func Container(image, secretName, serverUUID, dataVolumeName, dataMountPath string) corev1.Container {
	return corev1.Container{
		Name:  ContainerName,
		Image: image,
		Args: []string{
			fmt.Sprintf("--root=%s", dataMountPath),
			fmt.Sprintf("--listen=:%d", Port),
			fmt.Sprintf("--server-uuid=%s", serverUUID),
		},
		Env: []corev1.EnvVar{
			{Name: HMACKeyFileEnv, Value: secretMountPath + "/" + secretFileName},
		},
		Ports: []corev1.ContainerPort{
			{Name: PortName, ContainerPort: Port, Protocol: corev1.ProtocolTCP},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath},
			{Name: secretVolumeName, MountPath: secretMountPath, ReadOnly: true},
		},
	}
}

// SecretVolume builds the Volume that mounts the per-GameServer HMAC secret
// into the sftp-agent container.
func SecretVolume(secretName string) corev1.Volume {
	return corev1.Volume{
		Name: secretVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: secretName},
		},
	}
}

// SharedFSGroup is the Pod-level fsGroup both the GameServer controller
// (sidecar Pod) and the Panel API (maintenance Pod) set so the sftp-agent
// container — which runs as this same gid, see Dockerfile.sftp-agent — can
// read and write files the "server" container created and vice versa,
// regardless of which uid that container itself runs as. It matches the
// uid/gid most game-server base images (e.g. itzg/*) already use; an Egg
// whose image uses a different one is a known gap, not yet configurable —
// see AGENTS.md.
const SharedFSGroup int64 = 1000

// FSGroupPtr returns a pointer to SharedFSGroup, ready to assign directly to
// a corev1.PodSecurityContext.FSGroup field.
func FSGroupPtr() *int64 {
	v := SharedFSGroup
	return &v
}

// ServicePort builds the Service port that exposes the sftp-agent, whichever
// Pod (sidecar or maintenance) currently backs it.
func ServicePort() corev1.ServicePort {
	return corev1.ServicePort{
		Name:       PortName,
		Port:       Port,
		TargetPort: intstr.FromInt32(Port),
		Protocol:   corev1.ProtocolTCP,
	}
}
