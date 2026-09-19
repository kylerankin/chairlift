// Package progresslog provides a bounded rolling buffer of staging progress
// messages. The GTK UI creates one ActionRow per nonempty output line; without
// a cap, verbose staging accumulates thousands of heavyweight widgets and
// callbacks, freezing the main thread. This buffer owns the cap so the
// decision has headless regression coverage, and the views package only wires
// the buffer's overflow signal to row add/remove.
package progresslog

// DefaultCap is the maximum number of progress rows retained in the UI. Once
// exceeded, the oldest row is dropped as each new line arrives.
const DefaultCap = 200

// Buffer retains at most max recent messages, dropping the oldest once full.
// It is widget-free: the caller mirrors retention by removing the oldest UI
// row whenever Add reports an overflow.
type Buffer struct {
	max  int
	msgs []string
}

// New returns a Buffer that retains at most max messages. A non-positive max
// falls back to DefaultCap.
func New(max int) *Buffer {
	if max <= 0 {
		max = DefaultCap
	}
	return &Buffer{max: max}
}

// Add records msg and reports whether adding it overflowed the buffer — that
// is, the caller should now drop the oldest retained row from the UI so the
// row list stays aligned with the messages this buffer keeps.
func (b *Buffer) Add(msg string) bool {
	b.msgs = append(b.msgs, msg)
	if len(b.msgs) > b.max {
		// Drop the oldest; the caller removes the matching UI row.
		b.msgs = b.msgs[len(b.msgs)-b.max:]
		return true
	}
	return false
}

// Len returns the number of retained messages.
func (b *Buffer) Len() int { return len(b.msgs) }

// Messages returns a copy of the retained messages, oldest first.
func (b *Buffer) Messages() []string {
	out := make([]string, len(b.msgs))
	copy(out, b.msgs)
	return out
}
