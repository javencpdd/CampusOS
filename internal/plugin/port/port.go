package port

import "context"

type Descriptor struct{ ID, Version, Runtime string }
type Catalog interface {
	List(context.Context) ([]Descriptor, error)
	Get(context.Context, string) (Descriptor, error)
}
type RuntimeDispatcher interface {
	Dispatch(context.Context, string, string, []byte) ([]byte, error)
}
type Audit interface {
	Record(context.Context, string, string, map[string]interface{}) error
}
