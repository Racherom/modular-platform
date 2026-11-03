-- The four tables of the first managed thread (spec 0006, "Data model sketch").

-- +goose Up
CREATE TABLE sessions (
    token_hash   bytea       PRIMARY KEY CHECK (length(token_hash) = 32),
    iss          text        NOT NULL,
    sub          text        NOT NULL,
    email        text        NOT NULL,
    id_token     text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE admin_binding (
    singleton boolean     PRIMARY KEY DEFAULT true CHECK (singleton),
    iss       text        NOT NULL,
    sub       text        NOT NULL,
    email     text        NOT NULL,
    bound_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
    id                  uuid        PRIMARY KEY DEFAULT uuidv7(),
    name                text        NOT NULL UNIQUE CHECK (name <> 'modular-platform'),
    project             text        NOT NULL UNIQUE GENERATED ALWAYS AS ('mp-' || name) STORED,
    created_at          timestamptz NOT NULL DEFAULT now(),
    latest_operation_id uuid
);

CREATE TABLE operations (
    id                    uuid        PRIMARY KEY DEFAULT uuidv7(),
    deployment_id         uuid        NOT NULL REFERENCES deployments (id),
    action                text        NOT NULL CHECK (action IN ('apply', 'remove')),
    status                text        NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'unknown')),
    step                  text        CHECK (step IN ('prepare', 'pull', 'up', 'down', 'done')),
    graph                 jsonb,
    plan_digest           text,
    delete_data           boolean     NOT NULL DEFAULT false,
    rollback_of           uuid        REFERENCES operations (id),
    placed_supplied       text[]      NOT NULL DEFAULT '{}',
    started_by_iss        text        NOT NULL,
    started_by_sub        text        NOT NULL,
    started_by_email      text        NOT NULL,
    deployer_operation_id text,
    deployer_boot_id      text,
    error                 text,
    started_at            timestamptz NOT NULL DEFAULT now(),
    finished_at           timestamptz,
    CHECK ((action = 'apply') = (graph IS NOT NULL))
);
CREATE INDEX operations_deployment_started_idx ON operations (deployment_id, started_at DESC);
-- at most one running operation per deployment: the 409 busy rule, across api processes
CREATE UNIQUE INDEX operations_one_running_idx ON operations (deployment_id) WHERE status = 'running';

ALTER TABLE deployments
    ADD CONSTRAINT deployments_latest_operation_fk FOREIGN KEY (latest_operation_id)
    REFERENCES operations (id) DEFERRABLE INITIALLY DEFERRED;

-- +goose Down
ALTER TABLE deployments DROP CONSTRAINT deployments_latest_operation_fk;
DROP TABLE operations;
DROP TABLE deployments;
DROP TABLE admin_binding;
DROP TABLE sessions;
