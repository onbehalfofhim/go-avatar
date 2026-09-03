CREATE TABLE outbox_events (
    message_id UUID PRIMARY KEY,
    routing_key VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_outbox_events_pending
ON outbox_events (created_at)
WHERE published_at IS NULL;