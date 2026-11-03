-- name: CreateSession :exec
INSERT INTO sessions (token_hash, iss, sub, email, id_token, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: TouchSession :exec
-- at most once a minute per session
UPDATE sessions SET last_seen_at = now()
WHERE token_hash = $1 AND last_seen_at < now() - interval '1 minute';

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteStaleSessions :execrows
DELETE FROM sessions
WHERE expires_at <= now() OR last_seen_at <= now() - sqlc.arg(idle)::interval;

-- name: GetAdminBinding :one
SELECT * FROM admin_binding WHERE singleton;

-- name: BindAdmin :one
-- atomic: of two first sign ins at once, one inserts and the other reads the winner
WITH bound AS (
    INSERT INTO admin_binding (iss, sub, email) VALUES ($1, $2, $3)
    ON CONFLICT (singleton) DO NOTHING
    RETURNING iss, sub
)
SELECT iss, sub FROM bound
UNION ALL
SELECT iss, sub FROM admin_binding WHERE singleton
LIMIT 1;

-- name: EnsureDeployment :one
-- returns the deployment named $1, creating it when it does not exist yet
WITH created AS (
    INSERT INTO deployments (name) VALUES ($1)
    ON CONFLICT (name) DO NOTHING
    RETURNING id, project, true AS created
)
SELECT id, project, created FROM created
UNION ALL
SELECT id, project, false FROM deployments WHERE name = $1
LIMIT 1;

-- name: DeleteDeployment :exec
DELETE FROM deployments WHERE id = $1 AND latest_operation_id IS NULL;

-- name: InsertOperation :one
INSERT INTO operations (
    deployment_id, action, status, step, graph, plan_digest, delete_data, rollback_of,
    placed_supplied, started_by_iss, started_by_sub, started_by_email
) VALUES ($1, $2, 'running', 'prepare', $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: SetLatestOperation :exec
UPDATE deployments SET latest_operation_id = $2 WHERE id = $1;

-- name: ResetLatestOperation :exec
-- undoes SetLatestOperation for an operation the deployer refused
UPDATE deployments SET latest_operation_id = (
    SELECT o.id FROM operations o
    WHERE o.deployment_id = deployments.id AND o.id <> sqlc.arg(refused)
    ORDER BY o.started_at DESC, o.id DESC LIMIT 1
) WHERE deployments.id = sqlc.arg(deployment_id);

-- name: DeleteOperation :exec
DELETE FROM operations WHERE id = $1;

-- name: AcceptOperation :exec
UPDATE operations SET deployer_operation_id = $2, deployer_boot_id = $3
WHERE id = $1 AND status = 'running';

-- name: ProgressOperation :execrows
UPDATE operations SET step = $2 WHERE id = $1 AND status = 'running' AND step IS DISTINCT FROM $2;

-- name: FinishOperation :execrows
UPDATE operations SET status = $2, step = $3, error = $4, finished_at = now()
WHERE id = $1 AND status = 'running';

-- name: GetOperation :one
SELECT * FROM operations WHERE id = $1;

-- name: RunningOperations :many
SELECT sqlc.embed(o), d.name AS deployment_name, d.project
FROM operations o JOIN deployments d ON d.id = o.deployment_id
WHERE o.status = 'running'
ORDER BY o.started_at;

-- name: GetDeployment :one
SELECT d.id, d.name, d.project, d.created_at, d.latest_operation_id
FROM deployments d WHERE d.name = $1;

-- name: ListDeployments :many
SELECT d.id, d.name, d.project, d.created_at, d.latest_operation_id
FROM deployments d
WHERE d.name > sqlc.arg(after)
ORDER BY d.name
LIMIT sqlc.arg(max_rows);

-- name: ListOperations :many
SELECT o.* FROM operations o
WHERE o.deployment_id = sqlc.arg(deployment_id)
  AND (sqlc.narg(before)::uuid IS NULL
       OR (o.started_at, o.id) < (SELECT b.started_at, b.id FROM operations b WHERE b.id = sqlc.narg(before)::uuid))
ORDER BY o.started_at DESC, o.id DESC
LIMIT sqlc.arg(max_rows);

-- name: GetOperationsByIDs :many
SELECT * FROM operations WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: LatestSucceededApply :one
-- the graph the deployment runs now
SELECT * FROM operations
WHERE deployment_id = $1 AND action = 'apply' AND status = 'succeeded'
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: LatestApply :one
-- the graph Apply again reviews: the newest apply, whatever its status
SELECT * FROM operations
WHERE deployment_id = $1 AND action = 'apply'
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: LatestSucceededBefore :one
-- the version a rollback returns to: the newest succeeded operation before the failed apply
SELECT o.* FROM operations o
WHERE o.deployment_id = sqlc.arg(deployment_id) AND o.status = 'succeeded'
  AND (o.started_at, o.id) < (SELECT f.started_at, f.id FROM operations f WHERE f.id = sqlc.arg(failed))
ORDER BY o.started_at DESC, o.id DESC
LIMIT 1;

-- name: CountRollbacks :one
SELECT count(*) FROM operations WHERE rollback_of = $1;

-- name: LockLatestOperation :one
-- holds the deployment row, so its latest operation cannot change until the transaction ends
SELECT latest_operation_id FROM deployments WHERE id = $1 FOR UPDATE;

-- name: LatestApplyReachedUp :one
-- the graph whose instances the state read expects
SELECT * FROM operations
WHERE deployment_id = $1 AND action = 'apply' AND step IN ('up', 'done')
ORDER BY started_at DESC, id DESC
LIMIT 1;
