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

// Package sftpagent has the SFTP server (server.go) and the container/volume contract shared by the
// sidecar and the Panel's maintenance Pod (this file).
package sftpagent

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	// Data volume name and mount path shared by the controller and the Panel.
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

// SecretData builds the data of the per-GameServer HMAC Secret.
func SecretData(hmacKey []byte) map[string][]byte {
	return map[string][]byte{secretFileName: hmacKey}
}

// SecretName is the per-GameServer HMAC Secret's name.
func SecretName(gameServerName string) string {
	return gameServerName + "-sftp"
}

// Container builds the sftp-agent container; the caller declares the data volume.
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
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			RunAsNonRoot:             ptr.To(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
}

func PodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		FSGroup:        FSGroupPtr(),
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
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

// SharedFSGroup is the Pod fsGroup that lets the sftp-agent (this gid) and the game share files.
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
