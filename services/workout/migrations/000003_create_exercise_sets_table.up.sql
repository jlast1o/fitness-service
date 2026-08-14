CREATE TABLE exercise_sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workout_id UUID NOT NULL REFERENCES workouts(id) ON DELETE CASCADE,
    exercise_id UUID NOT NULL REFERENCES exercises(id),
    order_index INT NOT NULL DEFAULT 0,
    weight NUMERIC(8,2) NOT NULL DEFAULT 0,
    reps INT NOT NULL,
    rpe NUMERIC(3,1),
    metrics JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_exercise_sets_workout_id ON exercise_sets(workout_id);
CREATE INDEX idx_exercise_sets_exercise_id ON exercise_sets(exercise_id);