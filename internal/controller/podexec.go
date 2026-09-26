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

package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// NewPodExec builds a PodExecFunc that runs commands in a container over pods/exec, discarding
// stdout; a non-zero exit or a transport error comes back as the error (with stderr, when any,
// appended for context). Same approach as the Panel API's execInPod
// (internal/panelapi/runtime.go), but callers here don't need stdout back.
func NewPodExec(cfg *rest.Config, cs kubernetes.Interface) PodExecFunc {
	return func(ctx context.Context, namespace, pod, container string, cmd []string) error {
		req := cs.CoreV1().RESTClient().Post().
			Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: container,
				Command:   cmd,
				Stdout:    true,
				Stderr:    true,
			}, scheme.ParameterCodec)
		exec, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, req.URL())
		if err != nil {
			return err
		}
		var stderr bytes.Buffer
		if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: io.Discard, Stderr: &stderr}); err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
}

// stdinCommand builds the exec command that writes one line to the game's console (the stdin of
// PID 1). The command is an argument, never part of the script: nothing in it is interpreted.
func stdinCommand(command string) []string {
	return []string{"sh", "-c", `printf '%s\n' "$1" > /proc/1/fd/0`, "sh", command}
}
