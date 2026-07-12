package port

import "context"

type Category struct{ ID, Name string }
type Thread struct{ ID, CategoryID, AuthorID, Status string }
type Post struct{ ID, ThreadID, AuthorID, Status string }
type CategoryReader interface {
	GetCategory(context.Context, string) (Category, error)
}
type ThreadReader interface {
	GetThread(context.Context, string) (Thread, error)
}
type ThreadWriter interface {
	SetThreadStatus(context.Context, string, string) error
}
type PostReader interface {
	GetPost(context.Context, string) (Post, error)
}
type PostWriter interface {
	SetPostStatus(context.Context, string, string) error
}
