CREATE TABLE exercise_progress (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    exercise_id UUID NOT NULL,
    best_weight NUMERIC(8,2) NOT NULL DEFAULT 0,
    total_reps INT NOT NULL DEFAULT 0,
    last_workout_at TIMESTAMPTZ,
    estimated_1rm NUMERIC(8,2) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, exercise_id)
);

CREATE INDEX idx_exercise_progress_user_id ON exercise_progress(user_id);
CREATE INDEX idx_exercise_progress_exercise_id ON exercise_progress(exercise_id);