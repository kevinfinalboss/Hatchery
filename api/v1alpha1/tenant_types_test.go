package v1alpha1

import "testing"

func TestTenantNamespace(t *testing.T) {
	if got := TenantNamespace("acme"); got != "hatchery-acme" {
		t.Fatalf("TenantNamespace(acme) = %q, want hatchery-acme", got)
	}
}

func TestReservedTenantNamesMapOntoExistingNamespaces(t *testing.T) {
	if TenantNamespace("catalog") != CatalogNamespace {
		t.Fatalf("TenantNamespace(catalog) = %q, want CatalogNamespace (%q)", TenantNamespace("catalog"), CatalogNamespace)
	}
	if TenantNamespace("system") != OperatorNamespace {
		t.Fatalf("TenantNamespace(system) = %q, want OperatorNamespace (%q)", TenantNamespace("system"), OperatorNamespace)
	}
}
