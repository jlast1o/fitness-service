
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    event_type TEXT NOT NULL,
    event_version INTEGER NOT NULL CHECK (event_version > 0),
    payload JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,

    claimed_by UUID,
    claim_expires_at TIMESTAMPTZ,

    CONSTRAINT outbox_claim_pair_check CHECK (
        (claimed_by IS NULL AND claim_expires_at IS NULL)
        OR
        (claimed_by IS NOT NULL AND claim_expires_at IS NOT NULL)
    )
);

CREATE INDEX idx_outbox_events_published_at
    ON outbox_events(published_at)
    WHERE published_at IS NULL;

CREATE INDEX idx_outbox_events_claimable
    ON outbox_events(claim_expires_at, created_at, id)
    WHERE published_at IS NULL;
