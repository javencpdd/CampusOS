# CampusOS ER Diagram Generator

This generator reads the repository's real `migrations/*.up.sql` files and derives the ER model from DDL instead of maintaining a hand-drawn diagram.

## Run

From the CampusOS repository root:

```bash
python migrations/tool/generate_er.py --migrations migrations --out migrations/er
```

Graphviz is required only for rendering SVG/PDF/PNG. The parser itself has no third-party Python dependencies.

Ubuntu/Debian:

```bash
sudo apt install graphviz
```

Windows (winget):

```powershell
winget install Graphviz.Graphviz
```

Then rerun the generator.

## Outputs

- `campusos_er_full.svg` — all tables, all fields, PK/FK/UQ/NN markers, physical FK edges, cardinalities, ON DELETE semantics, inferred logical M:N edges.
- `campusos_er_overview.svg` — table/domain overview for architecture review.
- `domains/*.svg` — field-level ER diagrams split by business domain.
- `relations.md` — textual explanation of 1:1, 1:N and inferred M:N relationships.
- `schema.json` — machine-readable parsed schema.
- Corresponding `.dot` sources — editable vector graph source.

## Design choices

- 1:1 is inferred only when the FK column set is also a global PK/UNIQUE set.
- Nullable FKs are rendered as optional (`0..1` / `0..N`).
- M:N is shown as a dashed logical edge while retaining the two physical 1:N FK edges through the association table.
- Partial unique indexes are intentionally not used to infer global 1:1 relationships.
- Business-domain grouping is based on current CampusOS naming conventions and can be adjusted in `classify_table()`.

## Windows usage

### 1. Install Python 3

```powershell
winget install Python.Python.3.12
```

Close and reopen PowerShell, then verify:

```powershell
py -3 --version
```

### 2. Install Graphviz

```powershell
winget install Graphviz.Graphviz
```

Close and reopen PowerShell, then verify:

```powershell
dot -V
```

### 3. Put the generator in the CampusOS repository

Recommended layout:

```text
CampusOS/
├─ migrations/
├─ tools/
│  └─ db/
│     ├─ generate_er.py
│     └─ generate_er_windows.ps1
└─ docs/
```

### 4. Generate from migration UP scripts (recommended)

From the repository root:

```powershell
py -3 .\tools\db\generate_er.py --migrations .\migrations --out .\docs\database\er
```

Or use the wrapper:

```powershell
.\tools\db\generate_er_windows.ps1
```

### 5. Scan every SQL file that can define schema

This recursively reads every `*.sql` file under the directory but excludes `*.down.sql` by default:

```powershell
py -3 .\tools\db\generate_er.py --migrations .\migrations --out .\docs\database\er --sql-mode all
```

PowerShell wrapper equivalent:

```powershell
.\tools\db\generate_er_windows.ps1 -SqlMode all
```

Literal all-SQL mode, including rollback scripts, is available but normally should not be used to describe the active schema:

```powershell
.\tools\db\generate_er_windows.ps1 -SqlMode all -IncludeDown
```

The directory scan is recursive, so SQL files in nested subdirectories are included automatically.
