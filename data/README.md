# CampusOS Data Directory

> Last updated: 2026-11-15 (Asia/Shanghai).

`data/` is the default local data root. Its subdirectories have separate
owners and must not be used interchangeably.

| Directory | Purpose |
| --- | --- |
| `plugins/<id>/` | Legacy external-plugin data, retained for historical recovery; not a v5 source or automatic discovery root. Built-in modules are forbidden here. |
| `plugin_data/v5/packages/` | Current managed compressed v5 packages, addressed by immutable digest. Configured by `PLUGIN_DATA_DIR`. |
| `plugin_data/v5/repository/<publisher>/<key>/<version>/` | Temporary plugin repository: original compressed package, detached signature, manifest and backup metadata. No execution or restored authority. |
| `plugin_data/v5/{prepared,data,checkpoints,restored-data}/` | Host-private configuration capsules, plugin data and recovery views. |
| `plugin_data/<legacy-id>/` | Historical plugin KV, caches and snapshots. |
| `module_data/<feature>/` | Mutable local data owned by a compiled Built-in Feature. It is not packaged or removed by Plugin Manager. |
| `resources/<type>/<id>/` | Runtime-free Theme, Homepage, Space Style, Skill, Prompt, Persona and knowledge metadata packages. Each managed package uses `resource.json`. |
| `personal-space/<user-id>/` | User Storage Core root for avatars, rich-text images, schedules, documents and authorized plugin user files. |
| `images/` | Global local images without a user owner. |
| `dist/` | Local release or packaging output. |
| `config/` | Local runtime configuration. Do not commit secrets. |
| `skills/` | Legacy local/imported Skill storage. Repository development skills remain under the top-level `skills/`. |

Compiled module descriptors live in the repository-level `modules/` directory
and their Go implementations live under `internal/modules/`. They are not data
and are never installed through `data/plugins/`.

Validate this boundary with:

```bash
make data-governance-check
make architecture-check
```
