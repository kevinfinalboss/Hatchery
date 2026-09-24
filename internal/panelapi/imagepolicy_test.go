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
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserversv1alpha1 "github.com/kevinfinalboss/Hatchery/api/v1alpha1"
	"github.com/kevinfinalboss/Hatchery/internal/paneldb"
)

func testTenant(extra ...string) *gameserversv1alpha1.Tenant {
	return &gameserversv1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: testOrgSlug},
		Spec: gameserversv1alpha1.TenantSpec{Quota: gameserversv1alpha1.TenantQuota{
			CPU: resource.MustParse("4"), Memory: resource.MustParse("8Gi"), Storage: resource.MustParse("20Gi"),
			MaxGameServers: 3, ExtraImageRegistries: extra,
		}},
	}
}

func eggBodyWithImage(name, image string) map[string]any {
	return map[string]any{"name": name, "spec": map[string]any{
		"images": []map[string]any{{"name": "default", "image": image}}, "startCommand": "run",
	}}
}

func TestPrivateEggImageMustBeAllowed(t *testing.T) {
	srv := newTestServer(t, testTenant("quay.io/myteam"))
	srv.AllowedImageRegistries = []string{"docker.io/itzg"}
	adm := newMemberToken(t, srv, "a", paneldb.RoleAdmin)

	rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), adm, eggBodyWithImage("bad", "quay.io/evil/x:1"))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "quay.io/evil/x:1") {
		t.Fatalf("disallowed image: got %d %s, want 422 naming the image", rec.Code, rec.Body.String())
	}
	for _, img := range []string{"itzg/minecraft-server", "quay.io/myteam/server:2"} {
		if rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), adm, eggBodyWithImage("ok-"+strings.ReplaceAll(img[:4], ".", ""), img)); rec.Code != http.StatusCreated {
			t.Fatalf("allowed image %s: got %d %s", img, rec.Code, rec.Body.String())
		}
	}
}

func TestPrivateEggImageCheckWithoutTenantUsesDefaults(t *testing.T) {
	srv := newTestServer(t) // no Tenant object: defaults only, never a 500
	srv.AllowedImageRegistries = []string{"docker.io/itzg"}
	adm := newMemberToken(t, srv, "a", paneldb.RoleAdmin)
	if rec := doRequest(t, srv, http.MethodPost, orgURL("/eggs"), adm, eggBodyWithImage("x", "quay.io/evil/x")); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d %s, want 422", rec.Code, rec.Body.String())
	}
}

func TestCatalogEggsAreNotChecked(t *testing.T) {
	srv := newTestServer(t)
	srv.AllowedImageRegistries = []string{"docker.io/itzg"}
	platform := adminToken(t, srv)
	if rec := doRequest(t, srv, http.MethodPost, "/api/v1/catalog/eggs", platform, eggBodyWithImage("cat", "quay.io/any/x")); rec.Code != http.StatusCreated {
		t.Fatalf("catalog egg: got %d %s, want 201", rec.Code, rec.Body.String())
	}
}

func TestImagePolicyEndpoint(t *testing.T) {
	srv := newTestServer(t, testTenant("quay.io/myteam"))
	srv.AllowedImageRegistries = []string{"docker.io/itzg"}
	member := newMemberToken(t, srv, "m", paneldb.RoleMember)
	rec := doRequest(t, srv, http.MethodGet, orgURL("/image-policy"), member, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Enforced   bool     `json:"enforced"`
		Registries []string `json:"registries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enforced || strings.Join(got.Registries, ",") != "docker.io/itzg,quay.io/myteam" {
		t.Fatalf("got %+v", got)
	}
}

func TestQuotaCarriesExtraImageRegistries(t *testing.T) {
	srv := newTestServer(t, testTenant())
	platform := adminToken(t, srv)
	body := map[string]any{"cpu": "4", "memory": "8Gi", "storage": "20Gi", "maxGameServers": 3,
		"extraImageRegistries": []string{" quay.io/myteam ", ""}}
	rec := doRequest(t, srv, http.MethodPatch, orgURL("/quota"), platform, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"extraImageRegistries":["quay.io/myteam"]`) {
		t.Fatalf("response must echo the trimmed list: %s", rec.Body.String())
	}
	bad := map[string]any{"cpu": "4", "memory": "8Gi", "storage": "20Gi", "maxGameServers": 3,
		"extraImageRegistries": []string{"has space/x"}}
	if rec := doRequest(t, srv, http.MethodPatch, orgURL("/quota"), platform, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("entry with a space: got %d, want 400", rec.Code)
	}
}
