CREATE TABLE workout_summary (
    workout_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    name TEXT NOT NULL,
    date TIMESTAMPTZ NOT NULL,
    total_volume NUMERIC(12,2) NOT NULL DEFAULT 0,
    set_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_workout_summary_user_id ON workout_summary(user_id);