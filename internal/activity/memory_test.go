package activity

import (
	"testing"

	gosmo "github.com/radix29/gosmo"
)

// Each group fills from the clerk types SQL Server actually reports; a key
// that names no real type leaves its group empty and moves the memory to Other.
func TestCollectMemoryGroupsEveryKnownClerk(t *testing.T) {
	src := &fakeSource{clerks: []gosmo.MemoryClerk{
		{Type: "MEMORYCLERK_SQLBUFFERPOOL", MB: 1},
		{Type: "MEMORYCLERK_SQLGENERAL", MB: 2},
		{Type: "MEMORYCLERK_XTP", MB: 4},
		{Type: "CACHESTORE_SQLCP", MB: 8},
		{Type: "CACHESTORE_OBJCP", MB: 16},
		{Type: "CACHESTORE_COLUMNSTOREOBJECTPOOL", MB: 32},
		{Type: "MEMORYCLERK_SQLQERESERVATIONS", MB: 64},
		{Type: "SOMETHING_ELSE", MB: 128},
	}}
	got, err := collectMemory(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	want := []MemoryComponent{
		{memBuffer, 1}, {memStolen, 2}, {memInMemOLTP, 4}, {memPlanSQL, 8},
		{memPlanObjects, 16}, {memColumnstore, 32}, {memQueryGrants, 64}, {memOther, 128},
	}
	if len(got) != len(want) {
		t.Fatalf("collectMemory = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
}
