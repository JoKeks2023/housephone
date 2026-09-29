package store

import (
	"testing"
	"time"
)

func TestCompanionCodesReplaceAndDieWithParent(t *testing.T) {
	dir := t.TempDir()
	p := NewPairing(dir)
	now := time.Now()

	plain, err := p.Create("iPhone", now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.CreateCompanion("phone-1", "Watch", "watchos", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.CreateCompanion("phone-1", "Watch", "watchos", now)
	if err != nil {
		t.Fatal(err)
	}
	other, err := p.CreateCompanion("phone-2", "Watch", "watchos", now)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := p.Pending(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("pending %d codes, want 3 (the first companion code is replaced)", len(pending))
	}
	if _, err := p.Consume(first.Code, now); err == nil {
		t.Fatal("replaced companion code still valid")
	}

	if err := p.RemoveByParent("phone-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Consume(second.Code, now); err == nil {
		t.Fatal("companion code survived removal of its parent")
	}
	got, err := p.Consume(other.Code, now)
	if err != nil || got.ParentID != "phone-2" || got.Platform != "watchos" {
		t.Fatalf("other parent's code: %+v %v", got, err)
	}
	if _, err := p.Consume(plain.Code, now); err != nil {
		t.Fatalf("plain code: %v", err)
	}
}

func TestCompanionsLookup(t *testing.T) {
	d := NewDevices(t.TempDir())
	now := time.Now()
	for _, dev := range []Device{
		{ID: "phone", Platform: "ios", CreatedAt: now},
		{ID: "watch", Platform: "watchos", PairedBy: "phone", CreatedAt: now.Add(time.Second)},
		{ID: "other", Platform: "ios", CreatedAt: now.Add(2 * time.Second)},
	} {
		if err := d.Add(dev); err != nil {
			t.Fatal(err)
		}
	}
	companions, err := d.Companions("phone")
	if err != nil || len(companions) != 1 || companions[0].ID != "watch" {
		t.Fatalf("companions %+v %v", companions, err)
	}
	if none, _ := d.Companions("other"); len(none) != 0 {
		t.Fatalf("unexpected companions %+v", none)
	}
}
