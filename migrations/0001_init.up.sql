CREATE TABLE charge_forecasts (
    path_id     TEXT PRIMARY KEY,
    received_at TIMESTAMPTZ NOT NULL,
    buckets     JSONB NOT NULL
);

CREATE TABLE shift_plans (
    path_id            TEXT PRIMARY KEY,
    planned_heads      INTEGER NOT NULL,
    installed_stations INTEGER NOT NULL,
    rate_units_per_hr  DOUBLE PRECISION NOT NULL,
    hours              DOUBLE PRECISION NOT NULL
);

CREATE TABLE work_pools (
    path_id         TEXT PRIMARY KEY,
    mode            TEXT NOT NULL,
    wip_limit       INTEGER NOT NULL,
    alarm_threshold INTEGER NOT NULL
);

CREATE TABLE work_pool_entries (
    path_id      TEXT NOT NULL REFERENCES work_pools (path_id) ON DELETE CASCADE,
    work_unit_id TEXT NOT NULL,
    cpt          TIMESTAMPTZ NOT NULL,
    state        TEXT NOT NULL,
    PRIMARY KEY (path_id, work_unit_id)
);

CREATE TABLE work_units (
    id           TEXT PRIMARY KEY,
    path_id      TEXT NOT NULL,
    cpt          TIMESTAMPTZ NOT NULL,
    reference    TEXT NOT NULL,
    state        TEXT NOT NULL,
    released_at  TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_work_units_path_id ON work_units (path_id);

CREATE TABLE events (
    id           BIGSERIAL PRIMARY KEY,
    event_name   TEXT NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    payload      JSONB NOT NULL
);
