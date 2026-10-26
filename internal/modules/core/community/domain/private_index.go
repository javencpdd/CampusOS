package domain

// PrivateIndexFacts is a detached cleanup snapshot, never a content read grant.
type PrivateIndexFacts struct {
	Owner, Format, Revision string
	Epoch, Bytes            int64
	Available               bool
}
