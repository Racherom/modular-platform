-- An identity module whose users carry no email can bind the first admin (spec 0036, AC-38).

-- +goose Up
ALTER TABLE admin_binding ALTER COLUMN email DROP NOT NULL;

-- +goose Down
UPDATE admin_binding SET email = '' WHERE email IS NULL;
ALTER TABLE admin_binding ALTER COLUMN email SET NOT NULL;
