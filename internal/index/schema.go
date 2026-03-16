package index

// IndexSchema is the SQL DDL for the semantic index tables.
// These tables live in the project's SQLite database alongside the
// task/event tables. See Architecture Section 17.3.
const IndexSchema = `
CREATE TABLE IF NOT EXISTS idx_symbols (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,          -- function | type | interface | constant | variable
    file_path   TEXT NOT NULL,
    line        INTEGER NOT NULL,
    end_line    INTEGER,
    signature   TEXT,                   -- Full signature (e.g. "func HandleAuth(w http.ResponseWriter, r *http.Request) error")
    return_type TEXT,
    exported    BOOLEAN NOT NULL DEFAULT 0,
    parent_name TEXT,                   -- Enclosing type for methods
    package_path TEXT                   -- Package/module path
);

CREATE TABLE IF NOT EXISTS idx_fields (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol_id   INTEGER NOT NULL REFERENCES idx_symbols(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type_name   TEXT NOT NULL,
    exported    BOOLEAN NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS idx_imports (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path   TEXT NOT NULL,
    import_path TEXT NOT NULL,
    alias       TEXT                    -- Import alias, empty for default
);

CREATE TABLE IF NOT EXISTS idx_dependencies (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    from_pkg    TEXT NOT NULL,
    to_pkg      TEXT NOT NULL,
    dep_type    TEXT NOT NULL DEFAULT 'import'  -- import | reference
);

CREATE INDEX IF NOT EXISTS idx_symbols_name ON idx_symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON idx_symbols(file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON idx_symbols(kind);
CREATE INDEX IF NOT EXISTS idx_symbols_pkg ON idx_symbols(package_path);
CREATE INDEX IF NOT EXISTS idx_imports_file ON idx_imports(file_path);
CREATE INDEX IF NOT EXISTS idx_imports_path ON idx_imports(import_path);
CREATE INDEX IF NOT EXISTS idx_deps_from ON idx_dependencies(from_pkg);
CREATE INDEX IF NOT EXISTS idx_deps_to ON idx_dependencies(to_pkg);
`
