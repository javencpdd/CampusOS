# CampusOS compiled modules

This directory contains the checked-in descriptors for code compiled into the
CampusOS modular monolith. It is not an install directory.

```text
modules/
├── core/<id>/module.yaml       always-on platform capabilities
└── features/<id>/module.yaml   configurable built-in features
```

Implementation code lives under `internal/modules/`, except for the platform
kernel, Reliability Core, and Plugin Platform infrastructure explicitly named
by their module descriptor. Mutable module data belongs in `data/module_data/`;
user-owned files belong in `data/personal-space/`; themes and style packs
belong in `data/resources/`.

External extensions use `plugin.yaml` and live in `data/plugins/`. A directory
under `modules/` must never be packaged, installed, upgraded, or removed by the
Plugin Manager.

Validate the catalog and the physical boundary with:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./modules -count=1
make data-governance-check
make architecture-check
```
