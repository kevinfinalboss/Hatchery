package main

import "testing"

func TestParseServiceAccount(t *testing.T) {
	ns, name, err := parseServiceAccount("hatchery-panel/panel-api")
	if err != nil || ns != "hatchery-panel" || name != "panel-api" {
		t.Fatalf("got (%q, %q, %v)", ns, name, err)
	}
	if ns, name, err := parseServiceAccount(""); err != nil || ns != "" || name != "" {
		t.Fatalf("empty must mean disabled, got (%q, %q, %v)", ns, name, err)
	}
	for _, bad := range []string{"nonamespace", "/x", "x/", "a/b/c"} {
		if _, _, err := parseServiceAccount(bad); err == nil {
			t.Errorf("parseServiceAccount(%q) should fail", bad)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" 10.1.0.0/16, ,203.0.113.0/24 ")
	if len(got) != 2 || got[0] != "10.1.0.0/16" || got[1] != "203.0.113.0/24" {
		t.Fatalf("got %v", got)
	}
	if got := splitCSV(""); len(got) != 0 {
		t.Fatalf("empty input must give no entries, got %v", got)
	}
}

func TestParseCIDRList(t *testing.T) {
	got, err := parseCIDRList("10.1.0.0/16, 203.0.113.0/24")
	if err != nil || len(got) != 2 {
		t.Fatalf("got (%v, %v)", got, err)
	}
	if got, err := parseCIDRList(""); err != nil || len(got) != 0 {
		t.Fatalf("empty must be valid and give no entries, got (%v, %v)", got, err)
	}
	for _, bad := range []string{"10.1.0.0", "10.1.0.0/33", "10.1.0.0/16,nope"} {
		if _, err := parseCIDRList(bad); err == nil {
			t.Errorf("parseCIDRList(%q) should fail", bad)
		}
	}
}
