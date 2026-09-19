package progresslog

import "testing"

func TestBufferRetainsUpToCap(t *testing.T) {
	b := New(3)
	for _, m := range []string{"a", "b", "c"} {
		if b.Add(m) {
			t.Fatalf("Add(%q) overflowed at cap, want no overflow", m)
		}
	}
	if got := b.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	if got := b.Messages(); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("Messages() = %v, want [a b c]", got)
	}
}

func TestBufferDropsOldestWhenOverflowing(t *testing.T) {
	b := New(3)
	for _, m := range []string{"a", "b", "c"} {
		if b.Add(m) {
			t.Fatalf("Add(%q) overflowed before cap", m)
		}
	}
	// d overflows, dropping a.
	if !b.Add("d") {
		t.Fatal("Add('d') did not report overflow")
	}
	got := b.Messages()
	want := []string{"b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("Messages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Messages()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBufferNonPositiveMaxFallsBackToDefault(t *testing.T) {
	for _, max := range []int{0, -1} {
		b := New(max)
		if b.max != DefaultCap {
			t.Fatalf("New(%d).max = %d, want DefaultCap (%d)", max, b.max, DefaultCap)
		}
	}
}

func TestMessagesReturnsCopyNotBackingSlice(t *testing.T) {
	b := New(2)
	b.Add("a")
	first := b.Messages()
	first[0] = "mutated"
	if b.Messages()[0] != "a" {
		t.Fatal("Messages() returned the backing slice, not a copy")
	}
}
