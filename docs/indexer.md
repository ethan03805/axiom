# Semantic Indexer

The semantic indexer maintains a structured, queryable index of the project's code symbols, exports, interfaces, and dependency relationships. It enables the orchestrator to build precise TaskSpecs without reading raw source files.

---

## Purpose

Instead of dumping entire files into prompts, the orchestrator queries the semantic index to build minimum-necessary context for each TaskSpec. This reduces token usage, prevents context overload, and enables precise cross-reference tracking.

---

## Implementation

**Directory:** `internal/index/`

The indexer uses Go's `go/ast` parser for Go source files. Tree-sitter integration is planned for JavaScript/TypeScript, Python, and Rust.

### What Gets Indexed

| Entry Type | Data Extracted |
|-----------|----------------|
| **Functions** | Name, file, line, parameters, return type, exported/private |
| **Types/Structs** | Name, file, fields, methods, implements |
| **Interfaces** | Name, file, method signatures |
| **Constants/Variables** | Name, file, type, exported/private |
| **Imports** | File, imported package/module |
| **Exports** | Package/module, exported symbols |
| **Dependencies** | Package dependency graph |

### Index Storage

Index data is stored in SQLite tables within the project database:

- `idx_symbols` -- All named symbols with metadata
- `idx_fields` -- Struct/type fields
- `idx_imports` -- Import relationships
- `idx_dependencies` -- Package dependency graph

---

## Refresh Cycle

| Trigger | Scope |
|---------|-------|
| Project initialization | Full index of all source files |
| After each merge queue commit | Incremental: only changed files |
| `axiom index refresh` | Full re-index on demand |

The `.axiom/` directory is excluded from indexing. Internal Axiom files are not project code.

---

## Typed Query API

**File:** `internal/index/queries.go`

All queries use structured types. Natural language queries are not supported.

### lookup_symbol

Find a symbol by name and type.

```json
{
    "type": "query_index",
    "query_type": "lookup_symbol",
    "params": {
        "name": "HandleAuth",
        "type": "function"
    }
}
```

Response:
```json
{
    "results": [
        {
            "name": "HandleAuth",
            "file": "src/handlers/auth.go",
            "line": 23,
            "signature": "func HandleAuth(c *gin.Context)",
            "exported": true
        }
    ]
}
```

### reverse_dependencies

Find all files/symbols that reference a given symbol.

```json
{
    "type": "query_index",
    "query_type": "reverse_dependencies",
    "params": {
        "symbol_name": "HandleAuth"
    }
}
```

Response:
```json
{
    "results": [
        {"file": "src/routes/api.go", "line": 45, "symbol": "RegisterRoutes", "usage": "call"},
        {"file": "src/middleware/auth.go", "line": 12, "symbol": "AuthMiddleware", "usage": "reference"}
    ]
}
```

### list_exports

List all exported symbols in a package.

```json
{
    "type": "query_index",
    "query_type": "list_exports",
    "params": {
        "package_path": "src/handlers"
    }
}
```

### find_implementations

Find all types implementing an interface.

```json
{
    "type": "query_index",
    "query_type": "find_implementations",
    "params": {
        "interface_name": "UserRepository"
    }
}
```

### module_graph

Show the dependency graph from a root package or for the entire project.

```json
{
    "type": "query_index",
    "query_type": "module_graph",
    "params": {
        "root_package": "src/handlers"
    }
}
```

---

## Context Tiers

The orchestrator selects the minimum context tier for each TaskSpec:

| Tier | Description | Use Case |
|------|-------------|----------|
| **Symbol-level** | Specific function signatures, type definitions | Simple implementations against known interfaces |
| **File-level** | Complete relevant source files | Modifications to existing code |
| **Package-level** | Full package with internal dependencies | Refactoring, cross-file changes |
| **Repo map** | Dependency graph, directory structure, export index | Architectural tasks |
| **Indexed query** | Dynamic queries to the semantic index | On-demand context discovery |

Lower tiers are preferred. The orchestrator uses the semantic indexer to determine what context is needed and at what tier.

---

## IPC Integration

Orchestrators and sub-orchestrators query the index via `query_index` IPC messages. The engine validates query type and parameters, executes the query, and returns results in JSON format.

External orchestrators (Claws) query via the REST API: `POST /api/v1/index/query`.

---

## CLI Commands

```bash
# Force full re-index
axiom index refresh

# Query from CLI
axiom index query --type lookup_symbol --name HandleAuth
axiom index query --type reverse_dependencies --name UserRepository
axiom index query --type list_exports --package src/handlers
axiom index query --type find_implementations --name Repository
axiom index query --type module_graph
```

---

## Current Limitations

- Only Go source files are fully parsed (via `go/ast`)
- JavaScript/TypeScript, Python, and Rust support requires tree-sitter integration (planned)
- The indexer does not cross-reference across language boundaries
- Index refresh after large commits may take several seconds
