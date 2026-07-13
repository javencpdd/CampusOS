// Package runtime coordinates infrastructure profiles with the Module Registry.
// It deliberately knows ports and lifecycle only; it never constructs a domain
// Service, Handler, or route.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

type ProfileName string

const (
	ProfilePostgreSQL ProfileName = "postgresql"
	ProfileMemory     ProfileName = "memory"
)

// Profile supplies infrastructure adapters and explicitly named ports. A
// Profile may not construct domain application objects.
type Profile interface {
	Name() ProfileName
	Bind(*platformmodule.AppContext) error
	Close(context.Context) error
}

// Registration describes one module participating in an application runtime.
type Registration struct {
	Module  platformmodule.Module
	Kind    platformmodule.Kind
	Enabled bool
}

type Config struct {
	Profile    Profile
	Modules    []Registration
	AppContext *platformmodule.AppContext
}

// Runtime owns the application-wide lifecycle order: profile bind, module
// registration/start, reverse module stop, then profile close.
type Runtime struct {
	profile  Profile
	app      *platformmodule.AppContext
	registry *platformmodule.Registry

	mu      sync.Mutex
	started bool
	closed  bool
}

func New(config Config) (*Runtime, error) {
	if config.Profile == nil {
		return nil, errors.New("infrastructure profile is required")
	}
	if strings.TrimSpace(string(config.Profile.Name())) == "" {
		return nil, errors.New("infrastructure profile name is required")
	}
	app := config.AppContext
	if app == nil {
		app = platformmodule.NewAppContext()
	}
	registry := platformmodule.NewRegistry(app)
	for _, entry := range config.Modules {
		if err := registry.Add(entry.Module, entry.Kind, entry.Enabled); err != nil {
			return nil, err
		}
	}
	return &Runtime{profile: config.Profile, app: app, registry: registry}, nil
}

func (r *Runtime) ProfileName() ProfileName { return r.profile.Name() }

func (r *Runtime) AppContext() *platformmodule.AppContext { return r.app }

func (r *Runtime) Registry() *platformmodule.Registry { return r.registry }

func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("application runtime is closed")
	}
	if r.started {
		return nil
	}
	if err := r.profile.Bind(r.app); err != nil {
		closeErr := r.profile.Close(ctx)
		return errors.Join(fmt.Errorf("bind infrastructure profile %q: %w", r.profile.Name(), err), closeErr)
	}
	if err := r.registry.StartAll(ctx); err != nil {
		closeErr := r.profile.Close(ctx)
		return errors.Join(err, closeErr)
	}
	r.started = true
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var errs []error
	if r.started {
		if err := r.registry.StopAll(ctx); err != nil {
			errs = append(errs, err)
		}
		r.started = false
	}
	if err := r.profile.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("close infrastructure profile %q: %w", r.profile.Name(), err))
	}
	r.closed = true
	return errors.Join(errs...)
}

// StaticProfile is useful where a profile has no ports to bind yet. It still
// publishes the selected profile name, making PG/Memory selection observable
// without leaking adapter construction into Server.
type StaticProfile struct {
	name   ProfileName
	bind   func(*platformmodule.AppContext) error
	close  func(context.Context) error
	bound  bool
	closed bool
}

func NewStaticProfile(name ProfileName, bind func(*platformmodule.AppContext) error, close func(context.Context) error) *StaticProfile {
	return &StaticProfile{name: name, bind: bind, close: close}
}

func (p *StaticProfile) Name() ProfileName { return p.name }

func (p *StaticProfile) Bind(app *platformmodule.AppContext) error {
	if p.bound {
		return nil
	}
	if p.bind != nil {
		if err := p.bind(app); err != nil {
			return err
		}
	}
	p.bound = true
	return nil
}

func (p *StaticProfile) Close(ctx context.Context) error {
	if p.closed {
		return nil
	}
	p.closed = true
	if p.close != nil {
		return p.close(ctx)
	}
	return nil
}
