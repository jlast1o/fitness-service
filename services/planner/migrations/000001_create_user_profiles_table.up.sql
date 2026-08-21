CREATE TABLE user_profiles (
    user_id UUID PRIMARY KEY,
    goal TEXT NOT NULL,
    experience_level TEXT NOT NULL,
    days_per_week INT NOT NULL DEFAULT 3,
    injuries JSONB NOT NULL DEFAULT '{}',
    current_1rm JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);