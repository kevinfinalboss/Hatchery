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
	"net/http"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
)

type crashLogResponse struct {
	At        string `json:"at"`
	ExitCode  int32  `json:"exitCode"`
	Reason    string `json:"reason,omitempty"`
	OOMKilled bool   `json:"oomKilled"`
	Log       string `json:"log"`
}

// handleCrashLog serves the console tail the operator saved when the server's game process last crashed.
func (s *Server) handleCrashLog(w http.ResponseWriter, r *http.Request) {
	key := gameServerKey(r)
	var cm corev1.ConfigMap
	err := s.Client.Get(r.Context(), client.ObjectKey{Namespace: key.Namespace, Name: gameserversv1alpha1.CrashLogConfigMapName(key.Name)}, &cm)
	if apierrors.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "no crash recorded for this server")
		return
	}
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	exitCode, _ := strconv.ParseInt(cm.Data[gameserversv1alpha1.CrashLogKeyExitCode], 10, 32)
	oom, _ := strconv.ParseBool(cm.Data[gameserversv1alpha1.CrashLogKeyOOMKilled])
	writeJSON(w, http.StatusOK, crashLogResponse{
		At:        cm.Data[gameserversv1alpha1.CrashLogKeyAt],
		ExitCode:  int32(exitCode),
		Reason:    cm.Data[gameserversv1alpha1.CrashLogKeyReason],
		OOMKilled: oom,
		Log:       cm.Data[gameserversv1alpha1.CrashLogKeyLog],
	})
}
