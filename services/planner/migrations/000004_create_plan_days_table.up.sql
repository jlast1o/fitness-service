CREATE TABLE plan_days (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    week_id UUID NOT NULL REFERENCES plan_weeks(id) ON DELETE CASCADE,
    day_number INT NOT NULL,
    date DATE NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plan_days_week_id ON plan_days(week_id);