CREATE TABLE user_stats (
    user_id UUID PRIMARY KEY,
    total_workouts INT NOT NULL DEFAULT 0,
    total_volume NUMERIC(12,2) NOT NULL DEFAULT 0,
    avg_intensity NUMERIC(5,2) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);  