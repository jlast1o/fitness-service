CREATE INDEX idx_outbox_events_retention
    ON outbox_events (published_at, id)
    WHERE published_at IS NOT NULL;
