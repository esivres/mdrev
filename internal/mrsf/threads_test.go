package mrsf

import "testing"

func comments(cs ...Comment) *Sidecar { return &Sidecar{Comments: cs} }

// A reply written under another reply is accepted by the format and by the
// CLI, so dropping it from every view loses a human's answer while leaving it
// on disk.
func TestNestedRepliesStayInTheirThread(t *testing.T) {
	sc := comments(
		Comment{ID: "root", Text: "question"},
		Comment{ID: "a", ReplyTo: "root", Text: "answer"},
		Comment{ID: "b", ReplyTo: "a", Text: "follow-up"},
	)

	got := sc.Threads(false)
	if len(got) != 1 {
		t.Fatalf("want one thread, got %d", len(got))
	}
	if len(got[0].Replies) != 2 {
		t.Errorf("a reply to a reply must stay in the thread, got %d replies", len(got[0].Replies))
	}
}

// A reply whose parent was deleted by another tool must remain visible: an
// orphan is still something a person wrote.
func TestOrphanedReplyBecomesItsOwnThread(t *testing.T) {
	sc := comments(Comment{ID: "a", ReplyTo: "gone", Text: "answer"})

	if got := sc.Threads(false); len(got) != 1 || got[0].Parent.ID != "a" {
		t.Errorf("want the orphan shown as a thread, got %+v", got)
	}
}

// Resolved threads are hidden by default but must be reachable, or an agent
// cannot see the outcome of its own proposal.
func TestResolvedThreadsAreOptional(t *testing.T) {
	sc := comments(
		Comment{ID: "open", Text: "a"},
		Comment{ID: "done", Text: "b", Resolved: true},
	)

	if got := sc.Threads(false); len(got) != 1 {
		t.Errorf("resolved threads must be hidden by default, got %d", len(got))
	}
	if got := sc.Threads(true); len(got) != 2 {
		t.Errorf("resolved threads must be available on request, got %d", len(got))
	}
}

// A reply_to cycle must not hang the tool.
func TestCycleDoesNotHang(t *testing.T) {
	sc := comments(
		Comment{ID: "a", ReplyTo: "b"},
		Comment{ID: "b", ReplyTo: "a"},
	)
	if got := sc.Threads(false); len(got) == 0 {
		t.Error("a cycle must still yield something rather than nothing")
	}
}
