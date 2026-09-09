package mrsf

// Thread is a comment together with everything written under it.
type Thread struct {
	Parent  Comment
	Replies []Comment
}

// Threads groups a review into discussions. Replies nest arbitrarily deep in
// the format, and a reply may point at a parent that is no longer there, so
// every comment is walked up to its root: anything else drops a reply out of
// every view while leaving it in the file, which is how a human's answer went
// missing.
func (s *Sidecar) Threads(includeResolved bool) []Thread {
	byID := make(map[string]*Comment, len(s.Comments))
	for i := range s.Comments {
		byID[s.Comments[i].ID] = &s.Comments[i]
	}

	index := map[string]int{}
	var threads []Thread
	for i := range s.Comments {
		c := s.Comments[i]
		if root := rootOf(byID, c); root.ID != c.ID {
			continue
		}
		if c.Resolved && !includeResolved {
			continue
		}
		index[c.ID] = len(threads)
		threads = append(threads, Thread{Parent: c})
	}

	for i := range s.Comments {
		c := s.Comments[i]
		root := rootOf(byID, c)
		if root.ID == c.ID {
			continue
		}
		if at, ok := index[root.ID]; ok {
			threads[at].Replies = append(threads[at].Replies, c)
		}
	}
	return threads
}

// rootOf follows reply_to up to the comment that starts the thread. A reply
// whose parent is missing is its own root rather than lost. A cycle has no
// root at all, so each comment in it becomes its own — anything else drops the
// whole cycle out of every view.
func rootOf(byID map[string]*Comment, c Comment) Comment {
	start := c
	seen := map[string]bool{c.ID: true}
	for c.ReplyTo != "" {
		parent, ok := byID[c.ReplyTo]
		if !ok {
			return c
		}
		if seen[parent.ID] {
			return start
		}
		seen[parent.ID] = true
		c = *parent
	}
	return c
}
