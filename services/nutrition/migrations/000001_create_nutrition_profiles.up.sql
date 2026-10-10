CREATE TABLE nutrition_profiles (
    user_id UUID PRIMARY KEY,
    age INTEGER NOT NULL, 
    sex VARCHAR(20) NOT NULL,
    height_cm DOUBLE PRECISION NOT NULL,
    weight_kg DOUBLE PRECISION NOT NULL,
    activity_level VARCHAR(30) NOT NULL,
    goal VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);