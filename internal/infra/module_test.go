package infra

import "testing"

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	m := &stubModule{name: "test", displayName: "Test", description: "desc", status: StatusOK}
	reg.Register(m)

	got := reg.Get("test")
	if got == nil {
		t.Fatal("expected module, got nil")
	}
	if got.Name() != "test" {
		t.Errorf("expected test, got %s", got.Name())
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	reg := NewRegistry()
	if reg.Get("nope") != nil {
		t.Error("expected nil for unknown module")
	}
}

func TestRegistry_All_PreservesOrder(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubModule{name: "b", displayName: "B", description: "b", status: StatusOK})
	reg.Register(&stubModule{name: "a", displayName: "A", description: "a", status: StatusOK})
	reg.Register(&stubModule{name: "c", displayName: "C", description: "c", status: StatusOK})

	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	if all[0].Name() != "b" || all[1].Name() != "a" || all[2].Name() != "c" {
		t.Errorf("unexpected order: %s, %s, %s", all[0].Name(), all[1].Name(), all[2].Name())
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubModule{name: "x", displayName: "X", description: "x", status: StatusOK})
	reg.Register(&stubModule{name: "y", displayName: "Y", description: "y", status: StatusOK})

	names := reg.Names()
	if len(names) != 2 || names[0] != "x" || names[1] != "y" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubModule{name: "test", displayName: "V1", description: "v1", status: StatusOK})
	reg.Register(&stubModule{name: "test", displayName: "V2", description: "v2", status: StatusDegraded})

	// Should not duplicate in order.
	if len(reg.Names()) != 1 {
		t.Errorf("expected 1 name, got %d", len(reg.Names()))
	}

	// Should use the latest registration.
	got := reg.Get("test")
	if got.DisplayName() != "V2" {
		t.Errorf("expected V2, got %s", got.DisplayName())
	}
}

func TestModuleResult_Fields(t *testing.T) {
	m := &stubModule{name: "test", displayName: "Test", description: "desc", status: StatusDegraded, message: "slow"}
	result := m.Check(0)

	if result.Name != "test" {
		t.Errorf("expected test, got %s", result.Name)
	}
	if result.Status != StatusDegraded {
		t.Errorf("expected degraded, got %s", result.Status)
	}
	if result.Message != "slow" {
		t.Errorf("expected slow, got %s", result.Message)
	}
	if result.CheckedAt.IsZero() {
		t.Error("expected non-zero checked_at")
	}
}

// Compile-time check that stubModule (defined in handlers_test.go) satisfies Module.
var _ Module = (*stubModule)(nil)
