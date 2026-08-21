CREATE TABLE processed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE labor_plan_view (
    path_id       TEXT PRIMARY KEY,
    planned_heads INTEGER NOT NULL,
    planned_rate  DOUBLE PRECISION NOT NULL,
    planned_hours DOUBLE PRECISION NOT NULL,
    observed_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE usable_inventory_view (
    sku             TEXT PRIMARY KEY,
    usable_quantity INTEGER NOT NULL,
    observed_at     TIMESTAMPTZ NOT NULL
);
