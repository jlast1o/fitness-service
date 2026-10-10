
-- Каталог продуктов Nutrition Service.
-- owner_user_id IS NULL: общий продукт.
-- owner_user_id IS NOT NULL: личный продукт пользователя.
--
-- Не создаём внешний ключ на nutrition_profiles:
-- идентификаторы пользователей принадлежат сервису авторизации.

CREATE TABLE foods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    owner_user_id UUID,

    name TEXT NOT NULL,
    brand TEXT,

    -- Пищевая ценность всегда хранится на 100 граммов.
    -- NUMERIC позволяет хранить значения с фиксированной
    -- точностью до двух знаков после запятой.
    calories_per_100g NUMERIC(7, 2) NOT NULL,
    protein_per_100g NUMERIC(6, 2) NOT NULL,
    fat_per_100g NUMERIC(6, 2) NOT NULL,
    carbs_per_100g NUMERIC(6, 2) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Продукт можно скрыть, не удаляя его физически.
    -- Это полезно для сохранения истории дневника питания.
    deleted_at TIMESTAMPTZ,

    CONSTRAINT foods_name_valid
        CHECK (LENGTH(BTRIM(name)) BETWEEN 1 AND 200),

    CONSTRAINT foods_brand_valid
        CHECK (
            brand IS NULL
            OR LENGTH(BTRIM(brand)) BETWEEN 1 AND 120
        ),

    CONSTRAINT foods_calories_valid
        CHECK (calories_per_100g BETWEEN 0 AND 1000),

    CONSTRAINT foods_protein_valid
        CHECK (protein_per_100g BETWEEN 0 AND 100),

    CONSTRAINT foods_fat_valid
        CHECK (fat_per_100g BETWEEN 0 AND 100),

    CONSTRAINT foods_carbs_valid
        CHECK (carbs_per_100g BETWEEN 0 AND 100)
);

-- Индекс для поиска активных личных продуктов.
CREATE INDEX idx_foods_active_owner
    ON foods (owner_user_id)
    WHERE deleted_at IS NULL;

-- Индекс для поиска по нормализованному названию.
-- Сейчас подходит для точного сравнения без учёта регистра.
-- Подстрочный поиск оптимизируем отдельно при необходимости.
CREATE INDEX idx_foods_active_name
    ON foods (LOWER(name))
    WHERE deleted_at IS NULL;
