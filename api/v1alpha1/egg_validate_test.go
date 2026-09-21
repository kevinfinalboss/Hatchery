package v1alpha1

import "testing"

func TestEggSpecValidate(t *testing.T) {
	ok := EggSpec{StartupDetection: &EggStartupDetection{Regex: `\)! For help, type `}}
	if msgs := ok.Validate(); len(msgs) != 0 {
		t.Fatalf("valid regex: got %v", msgs)
	}
	if msgs := (&EggSpec{}).Validate(); len(msgs) != 0 {
		t.Fatalf("no detection is valid: got %v", msgs)
	}
	bad := EggSpec{StartupDetection: &EggStartupDetection{Regex: `(unclosed`}}
	if msgs := bad.Validate(); len(msgs) != 1 {
		t.Fatalf("invalid regex must be reported once, got %v", msgs)
	}
}
