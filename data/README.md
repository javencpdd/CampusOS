# CampusOS Data Directory

`data/` is the default local data root for runtime and distributable CampusOS assets.

| Directory | Purpose |
| --- | --- |
| `plugins/` | Installed and built-in plugin directories. The server scans this path by default through `PLUGINS_DIR=data/plugins`; built-ins currently include `personal-space` and `homepage-customizer`. |
| `plugin_data/` | Plugin runtime KV data, including SQLite-backed Host API storage. This path is configured by `PLUGIN_DATA_DIR=data/plugin_data`. |
| `images/` | Local image, avatar, and upload assets. `personal-space` stores default personal files under `images/personal-space/`. |
| `dist/` | Release or deployment artifacts reserved for local packaging output. |
| `config/` | Local runtime configuration files. Do not commit secrets. |
| `skills/` | Local runtime or imported skill assets. Project workflow skills remain in the repository-level `skills/` directory. |

Runtime data directories keep only placeholder files in git. Plugin source directories under `data/plugins/` are versioned when they are bundled with the project.
