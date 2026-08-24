CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE COLLATE NOCASE,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('coach','safety_officer','guardian','health_professional')),
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX sessions_user_active_idx ON sessions(user_id, expires_at, revoked_at);

CREATE TABLE participants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    public_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    birth_date TEXT NOT NULL,
    guardian_user_id INTEGER NOT NULL REFERENCES users(id),
    venue_id TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    created_at TEXT NOT NULL
);
CREATE INDEX participants_guardian_idx ON participants(guardian_user_id, active);
CREATE INDEX participants_venue_idx ON participants(venue_id, active);

CREATE TABLE incidents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    public_id TEXT NOT NULL UNIQUE,
    participant_id INTEGER NOT NULL REFERENCES participants(id),
    reporter_user_id INTEGER NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL,
    body_area TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL,
    severity TEXT NOT NULL,
    stop_training INTEGER NOT NULL CHECK(stop_training IN (0,1)),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX incidents_participant_status_idx ON incidents(participant_id, status, occurred_at DESC);
CREATE INDEX incidents_status_updated_idx ON incidents(status, updated_at DESC);

CREATE TABLE incident_revisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    revision INTEGER NOT NULL,
    body_area TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    description TEXT NOT NULL,
    reason TEXT NOT NULL,
    corrected_by INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL,
    UNIQUE(incident_id, revision)
);

CREATE TABLE triage_assessments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    safety_officer_id INTEGER NOT NULL REFERENCES users(id),
    severity TEXT NOT NULL,
    stop_training INTEGER NOT NULL CHECK(stop_training IN (0,1)),
    public_guidance TEXT NOT NULL,
    clinical_notes TEXT NOT NULL,
    assessed_at TEXT NOT NULL,
    UNIQUE(incident_id, assessed_at)
);

CREATE TABLE guardian_notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    guardian_user_id INTEGER NOT NULL REFERENCES users(id),
    channel TEXT NOT NULL,
    message_class TEXT NOT NULL,
    status TEXT NOT NULL,
    acknowledged_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(incident_id, guardian_user_id, message_class)
);

CREATE TABLE referrals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    organization TEXT NOT NULL,
    reason TEXT NOT NULL,
    status TEXT NOT NULL,
    returned_reason TEXT NOT NULL DEFAULT '',
    professional_id INTEGER REFERENCES users(id),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX referrals_incident_status_idx ON referrals(incident_id, status);

CREATE TABLE rehab_plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    referral_id INTEGER NOT NULL UNIQUE REFERENCES referrals(id),
    professional_id INTEGER NOT NULL REFERENCES users(id),
    current_version INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE rehab_plan_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id INTEGER NOT NULL REFERENCES rehab_plans(id),
    version INTEGER NOT NULL,
    goals TEXT NOT NULL,
    restrictions TEXT NOT NULL,
    exercises TEXT NOT NULL,
    review_due_at TEXT NOT NULL,
    published_by INTEGER NOT NULL REFERENCES users(id),
    published_at TEXT NOT NULL,
    UNIQUE(plan_id, version)
);

CREATE TABLE followups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id INTEGER NOT NULL REFERENCES rehab_plans(id),
    plan_version INTEGER NOT NULL,
    professional_id INTEGER NOT NULL REFERENCES users(id),
    pain_score INTEGER NOT NULL,
    mobility_score INTEGER NOT NULL,
    load_tolerance INTEGER NOT NULL,
    notes TEXT NOT NULL,
    assessed_at TEXT NOT NULL,
    valid_until TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY(plan_id, plan_version) REFERENCES rehab_plan_versions(plan_id, version)
);

CREATE TABLE clearances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    followup_id INTEGER NOT NULL REFERENCES followups(id),
    professional_id INTEGER NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL,
    conditions TEXT NOT NULL,
    status TEXT NOT NULL,
    valid_from TEXT NOT NULL,
    valid_until TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX clearances_incident_validity_idx ON clearances(incident_id, status, valid_from, valid_until);

CREATE TABLE training_blocks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id INTEGER NOT NULL REFERENCES participants(id),
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    reason TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    created_at TEXT NOT NULL,
    released_at TEXT,
    UNIQUE(incident_id, active)
);
CREATE INDEX training_blocks_participant_idx ON training_blocks(participant_id, active);

CREATE TABLE schedule_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    participant_id INTEGER NOT NULL REFERENCES participants(id),
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    requested_by INTEGER NOT NULL REFERENCES users(id),
    session_starts_at TEXT NOT NULL,
    conditions_acknowledged INTEGER NOT NULL CHECK(conditions_acknowledged IN (0,1)),
    allowed INTEGER NOT NULL CHECK(allowed IN (0,1)),
    decision_code TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE overrides (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id INTEGER NOT NULL REFERENCES incidents(id),
    granted_by INTEGER NOT NULL REFERENCES users(id),
    reason TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    notification_id INTEGER REFERENCES guardian_notifications(id),
    created_at TEXT NOT NULL
);

CREATE TABLE notification_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    notification_id INTEGER NOT NULL UNIQUE REFERENCES guardian_notifications(id),
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    next_attempt_at TEXT NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX notification_jobs_due_idx ON notification_jobs(status, next_attempt_at, lease_until);

CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id INTEGER NOT NULL,
    actor_role TEXT NOT NULL,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    result TEXT NOT NULL,
    reason TEXT NOT NULL,
    request_id TEXT NOT NULL,
    metadata_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX audit_object_idx ON audit_events(object_type, object_id, created_at DESC);
CREATE INDEX audit_actor_idx ON audit_events(actor_id, created_at DESC);

CREATE TABLE idempotency_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id INTEGER NOT NULL REFERENCES users(id),
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_code INTEGER NOT NULL,
    response_body TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    UNIQUE(actor_id, method, path, key)
);
