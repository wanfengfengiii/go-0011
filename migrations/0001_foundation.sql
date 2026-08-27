PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    site_code TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pour_sections (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    location TEXT NOT NULL,
    planned_pour_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mix_designs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    code TEXT NOT NULL,
    design_strength_mpa REAL NOT NULL,
    material_revision TEXT NOT NULL,
    UNIQUE (project_id, code)
);

CREATE TABLE IF NOT EXISTS inspection_rules (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    revision INTEGER NOT NULL,
    rule_json BLOB NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (project_id, revision)
);

CREATE TABLE IF NOT EXISTS sample_groups (
    id TEXT PRIMARY KEY,
    pour_section_id TEXT NOT NULL REFERENCES pour_sections(id),
    mix_design_id TEXT NOT NULL REFERENCES mix_designs(id),
    rule_json BLOB NOT NULL,
    status TEXT NOT NULL,
    version INTEGER NOT NULL,
    frozen_snapshot_id TEXT NOT NULL DEFAULT '',
    review_count INTEGER NOT NULL DEFAULT 0,
    sealed_conclusion TEXT NOT NULL DEFAULT '',
    sealed_at TEXT,
    sealed_digest TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS specimens (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES sample_groups(id),
    specimen_no TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    nominal_side_mm INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 0,
    bound_identity TEXT NOT NULL DEFAULT '',
    last_applied_at TEXT,
    max_seen_at TEXT,
    effective_age_minutes INTEGER NOT NULL DEFAULT 0,
    validity TEXT NOT NULL DEFAULT 'VALID',
    current_location TEXT NOT NULL DEFAULT '',
    UNIQUE (project_id, specimen_no)
);

CREATE TABLE IF NOT EXISTS specimen_events (
    global_position INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    source TEXT NOT NULL,
    specimen_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    received_at TEXT NOT NULL,
    expected_version INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    canonical_payload BLOB NOT NULL,
    payload_digest TEXT NOT NULL,
    applied_status TEXT NOT NULL,
    classified_error TEXT,
    UNIQUE (source, specimen_id, sequence)
);

CREATE TABLE IF NOT EXISTS aggregate_versions (
    aggregate_id TEXT PRIMARY KEY,
    aggregate_kind TEXT NOT NULL,
    version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS idempotency_receipts (
    identity_key TEXT PRIMARY KEY,
    payload_digest TEXT NOT NULL,
    status TEXT NOT NULL,
    specimen_version INTEGER NOT NULL,
    watermark TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS pending_events (
    event_id TEXT PRIMARY KEY REFERENCES specimen_events(event_id),
    specimen_id TEXT NOT NULL,
    sort_time TEXT NOT NULL,
    business_priority INTEGER NOT NULL,
    source TEXT NOT NULL,
    sequence INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS specimen_watermarks (
    specimen_id TEXT PRIMARY KEY,
    closed_until TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pressure_results (
    specimen_id TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL,
    curve_digest TEXT NOT NULL,
    peak_load_kn REAL NOT NULL,
    side_mm INTEGER NOT NULL,
    factor REAL NOT NULL,
    strength_mpa REAL NOT NULL,
    validity TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS evaluation_snapshots (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    parent_snapshot_id TEXT,
    group_version INTEGER NOT NULL,
    canonical_json BLOB NOT NULL,
    canonical_digest TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
    global_position INTEGER PRIMARY KEY,
    aggregate_digest TEXT NOT NULL,
    snapshot_blob BLOB NOT NULL,
    created_at TEXT NOT NULL
);
