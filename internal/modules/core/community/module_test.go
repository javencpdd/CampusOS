package community

import (
	"context"
	"testing"

	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	platformfeature "github.com/campusos/CampusOS/internal/platform/feature"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/pkg/cache"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

func TestMemoryProfileAndModuleExposePortsAndHandlers(t *testing.T) {
	app := platformmodule.NewAppContext()
	if err := BindMemoryAdapters(app); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide(portEventBus, eventbus.NewMemoryEventBus()); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide(portMemoryEventBus, eventbus.NewMemoryEventBus()); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide(portCache, cache.NewMemoryCache()); err != nil {
		t.Fatal(err)
	}
	features := platformfeature.NewRegistry(nil)
	if err := features.Register(platformfeature.Definition{ID: "controlled-richtext-article", Mode: platformfeature.Restart, DefaultEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := app.Provide("platform.feature-registry", features); err != nil {
		t.Fatal(err)
	}
	module := NewModule()
	if err := module.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := module.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if module.Handlers().Thread == nil || module.Handlers().Category == nil || module.Handlers().Post == nil || module.Handlers().Notification == nil || module.Handlers().Event == nil {
		t.Fatal("community module did not construct its handlers")
	}
	for _, name := range []string{portCategoryReader, portThreadPort, portPostPort} {
		value, ok := app.Lookup(name)
		if !ok {
			t.Fatalf("missing public port %s", name)
		}
		switch name {
		case portCategoryReader:
			if _, ok := value.(communityport.CategoryReader); !ok {
				t.Fatalf("unexpected category reader type %T", value)
			}
		case portThreadPort:
			if _, ok := value.(communityport.ThreadReader); !ok {
				t.Fatalf("unexpected thread port type %T", value)
			}
			if _, ok := value.(communityport.ThreadWriter); !ok {
				t.Fatalf("unexpected thread writer type %T", value)
			}
		case portPostPort:
			if _, ok := value.(communityport.PostReader); !ok {
				t.Fatalf("unexpected post port type %T", value)
			}
			if _, ok := value.(communityport.PostWriter); !ok {
				t.Fatalf("unexpected post writer type %T", value)
			}
		}
	}
}
