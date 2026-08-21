CREATE TABLE planned_exercises (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    day_id UUID NOT NULL REFERENCES plan_days(id) ON DELETE CASCADE,
    exercise_id UUID NOT NULL,
    target_sets INT NOT NULL,
    target_reps_min INT NOT NULL,
    target_reps_max INT NOT NULL,
    target_weight NUMERIC(8,2) NOT NULL DEFAULT 0,
    target_rpe NUMERIC(3,1),
    notes TEXT,
    order_index INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_planned_exercises_day_id ON planned_exercises(day_id);
CREATE INDEX idx_planned_exercises_exercise_id ON planned_exercises(exercise_id);