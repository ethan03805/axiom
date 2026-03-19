-- Axiom initial schema migration
-- Tasks: durable identity and metadata
CREATE TABLE tasks (
    id              TEXT PRIMARY KEY,
    parent_id       TEXT REFERENCES tasks(id),
    title           TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'queued',
    tier            TEXT NOT NULL,
    task_type       TEXT NOT NULL DEFAULT 'implementation',
    base_snapshot   TEXT,
    eco_ref         TEXT REFERENCES eco_log(id),
    blocked_by_task_id TEXT REFERENCES tasks(id),
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at    DATETIME
);

CREATE TABLE task_srs_refs (
    task_id     TEXT NOT NULL REFERENCES tasks(id),
    srs_ref     TEXT NOT NULL,
    PRIMARY KEY (task_id, srs_ref)
);

CREATE TABLE task_dependencies (
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    depends_on TEXT NOT NULL REFERENCES tasks(id),
    PRIMARY KEY (task_id, depends_on)
);

CREATE TABLE task_target_files (
    task_id     TEXT NOT NULL REFERENCES tasks(id),
    file_path   TEXT NOT NULL,
    lock_scope  TEXT NOT NULL DEFAULT 'file',
    PRIMARY KEY (task_id, file_path)
);

CREATE TABLE task_locks (
    resource_type TEXT NOT NULL,
    resource_key  TEXT NOT NULL,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    locked_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (resource_type, resource_key)
);

CREATE TABLE task_attempts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id         TEXT NOT NULL REFERENCES tasks(id),
    attempt_number  INTEGER NOT NULL,
    model_id        TEXT NOT NULL,
    model_family    TEXT NOT NULL,
    base_snapshot   TEXT NOT NULL,
    status          TEXT NOT NULL,
    input_tokens    INTEGER,
    output_tokens   INTEGER,
    cost_usd        REAL DEFAULT 0,
    failure_reason  TEXT,
    feedback        TEXT,
    started_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at    DATETIME
);

CREATE TABLE validation_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id      INTEGER NOT NULL REFERENCES task_attempts(id),
    check_type      TEXT NOT NULL,
    status          TEXT NOT NULL,
    output          TEXT,
    duration_ms     INTEGER,
    timestamp       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE review_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id      INTEGER NOT NULL REFERENCES task_attempts(id),
    reviewer_model  TEXT NOT NULL,
    reviewer_family TEXT NOT NULL,
    verdict         TEXT NOT NULL,
    feedback        TEXT,
    cost_usd        REAL DEFAULT 0,
    timestamp       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE task_artifacts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id      INTEGER NOT NULL REFERENCES task_attempts(id),
    file_path       TEXT NOT NULL,
    operation       TEXT NOT NULL,
    sha256          TEXT,
    size_bytes      INTEGER,
    timestamp       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE container_sessions (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id),
    container_type  TEXT NOT NULL,
    image           TEXT NOT NULL,
    model_id        TEXT,
    cpu_limit       REAL,
    mem_limit       TEXT,
    started_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    stopped_at      DATETIME,
    exit_reason     TEXT
);

CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type      TEXT NOT NULL,
    task_id         TEXT,
    agent_type      TEXT,
    agent_id        TEXT,
    details         TEXT,
    timestamp       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE cost_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id         TEXT REFERENCES tasks(id),
    attempt_id      INTEGER REFERENCES task_attempts(id),
    agent_type      TEXT NOT NULL,
    model_id        TEXT NOT NULL,
    input_tokens    INTEGER,
    output_tokens   INTEGER,
    cost_usd        REAL NOT NULL,
    timestamp       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE eco_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    eco_code        TEXT NOT NULL,
    category        TEXT NOT NULL,
    description     TEXT NOT NULL,
    affected_refs   TEXT NOT NULL,
    proposed_change TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'proposed',
    approved_by     TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at     DATETIME
);
