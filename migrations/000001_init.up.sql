CREATE SCHEMA IF NOT EXISTS webhook;

CREATE TABLE webhook.subscriptions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_url TEXT NOT NULL CHECK (target_url ~ '^https?://[^/[:space:]]+'),
    secret_key TEXT NOT NULL CHECK (length(secret_key) >= 26),
    event_types TEXT[] NOT NULL
        CHECK (cardinality(event_types) > 0)
        CHECK (array_position(event_types, '') IS NULL),
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_active_event_types_idx ON webhook.subscriptions USING GIN (event_types) WHERE active = true;

CREATE TABLE webhook.events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (length(event_type) > 0),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    idempotency_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT events_idempotency_key_unique UNIQUE (idempotency_key)
);

CREATE TABLE webhook.deliveries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES webhook.events(id) ON DELETE RESTRICT,
    subscription_id BIGINT NOT NULL REFERENCES webhook.subscriptions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'in_progress', 'delivered', 'failed', 'dead')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    last_status_code INT CHECK (last_status_code BETWEEN 100 AND 599),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT deliveries_event_subscription_unique UNIQUE (event_id, subscription_id),
    CONSTRAINT deliveries_lease_status_consistency CHECK (
        (status = 'in_progress' AND locked_until IS NOT NULL)
        OR
        (status <> 'in_progress' AND locked_until IS NULL)
    )
);

CREATE INDEX deliveries_due_idx ON webhook.deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX deliveries_lease_idx ON webhook.deliveries (locked_until) WHERE status = 'in_progress';
