CREATE TABLE plan_weeks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID NOT NULL REFERENCES training_plans(id) ON DELETE CASCADE,
    week_number INT NOT NULL,
    focus TEXT NOT NULL DEFAULT 'volume',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plan_weeks_plan_id ON plan_weeks(plan_id);