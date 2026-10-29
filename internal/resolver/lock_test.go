package resolver

import (
	"encoding/json"
	"testing"
)

func TestStandaloneLockValidation(t *testing.T) {
	b := load(t, "expected.lock.json")
	l, id, err := ReadLock(b, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, got, err := ReadLock(b, id); err != nil || got != id {
		t.Fatal(err)
	}
	l.Reasons = l.Reasons[:len(l.Reasons)-1]
	changed, err := canonical(l)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadLock(changed, ""); err == nil {
		t.Fatal("accepted false reason graph")
	}
	var pretty any
	if err := json.Unmarshal(b, &pretty); err != nil {
		t.Fatal(err)
	}
	formatted, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadLock(formatted, ""); err == nil {
		t.Fatal("accepted noncanonical bytes")
	}
}
