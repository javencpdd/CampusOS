# Category Moderation Core

Moderation is an always-on Core Module implemented in
`internal/modules/core/moderation`. Its category-scope authorization, audit,
and integrity checks cannot be uninstalled or disabled.

Administrators assign the `moderator` role together with one or more category
scopes. A moderator can act only on content whose server-resolved category is
in that assignment. Client-supplied category IDs are never trusted for the
authorization decision.

The following operation switches are configurable through the Feature API and
Admin built-in feature page:

- `allow_pin`
- `allow_lock`
- `allow_delete_post`

These switches can reduce the actions available to moderators, but cannot
grant role management, user administration, plugin management, or access to
other categories. Changing them does not unload the Core policy or delete
assignments and audit records.

`category-moderation` remains an API compatibility alias. It is not an
external plugin name and cannot be installed under `data/plugins`.
