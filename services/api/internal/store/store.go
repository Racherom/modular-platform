// Package store is api's persistence in its Postgres database: the goose migrations, the sqlc
// queries and the adapters behind the interfaces of packages signin and deploy.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/signin"
	"github.com/racherom/modular-platform/services/api/internal/store/queries"
)

//go:generate go tool sqlc generate -f sqlc.yaml

//go:embed migrations/*.sql
var migrations embed.FS

// uniqueViolation is Postgres' SQLSTATE for a unique index conflict.
const uniqueViolation = "23505"

// Store implements signin.Store and deploy.Store over a pgx pool.
type Store struct {
	pool *pgxpool.Pool
	q    *queries.Queries
}

var (
	_ signin.Store = (*Store)(nil)
	_ deploy.Store = (*Store)(nil)
)

// New returns the store over pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: queries.New(pool)} }

// Migrate applies every pending migration. A Postgres session lock makes concurrent starts of
// api safe (spec 0001: goose's default provider does not lock).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, mustSub(migrations, "migrations"), goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func found[T any](v T, err error) (T, bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		var zero T
		return zero, false, nil
	}
	if err != nil {
		var zero T
		return zero, false, err
	}
	return v, true, nil
}

// CreateSession stores s.
func (s *Store) CreateSession(ctx context.Context, sess signin.Session) error {
	return s.q.CreateSession(ctx, queries.CreateSessionParams{
		TokenHash: sess.TokenHash, Iss: sess.Identity.Issuer, Sub: sess.Identity.Subject,
		Email: sess.Email, IDToken: optional(sess.IDToken), ExpiresAt: sess.ExpiresAt,
	})
}

// Session returns the session with the token hash.
func (s *Store) Session(ctx context.Context, tokenHash []byte) (signin.Session, bool, error) {
	row, ok, err := found(s.q.GetSession(ctx, tokenHash))
	if !ok {
		return signin.Session{}, false, err
	}
	return signin.Session{
		TokenHash: row.TokenHash, Identity: signin.Identity{Issuer: row.Iss, Subject: row.Sub}, Email: row.Email,
		IDToken: deref(row.IDToken), CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt, ExpiresAt: row.ExpiresAt,
	}, true, nil
}

// TouchSession records activity at most once a minute.
func (s *Store) TouchSession(ctx context.Context, tokenHash []byte) error {
	return s.q.TouchSession(ctx, tokenHash)
}

// DeleteSession deletes a session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	return s.q.DeleteSession(ctx, tokenHash)
}

// DeleteStaleSessions deletes expired and idle sessions.
func (s *Store) DeleteStaleSessions(ctx context.Context, idle time.Duration) (int64, error) {
	return s.q.DeleteStaleSessions(ctx, pgtype.Interval{Microseconds: idle.Microseconds(), Valid: true})
}

// AdminBinding returns the bound admin.
func (s *Store) AdminBinding(ctx context.Context) (signin.Identity, bool, error) {
	row, ok, err := found(s.q.GetAdminBinding(ctx))
	if !ok {
		return signin.Identity{}, false, err
	}
	return signin.Identity{Issuer: row.Iss, Subject: row.Sub}, true, nil
}

// BindAdmin binds id when no admin is bound and returns whoever is bound.
func (s *Store) BindAdmin(ctx context.Context, id signin.Identity, email string) (signin.Identity, error) {
	row, err := s.q.BindAdmin(ctx, queries.BindAdminParams{Iss: id.Issuer, Sub: id.Subject, Email: email})
	if err != nil {
		return signin.Identity{}, err
	}
	return signin.Identity{Issuer: row.Iss, Subject: row.Sub}, nil
}

// ResetAdmin deletes the admin binding and every session (spec 0006 AC-6).
func (s *Store) ResetAdmin(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM admin_binding"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM sessions")
		return err
	})
}

// EnsureDeployment returns the deployment named name, creating it when missing.
func (s *Store) EnsureDeployment(ctx context.Context, name string) (uuid.UUID, string, bool, error) {
	row, err := s.q.EnsureDeployment(ctx, name)
	return row.ID, row.Project, row.Created, err
}

// DropNewDeployment deletes a deployment without operations.
func (s *Store) DropNewDeployment(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteDeployment(ctx, id)
}

// StartOperation inserts op as running and makes it latest; the partial unique index turns a
// second running operation into deploy.ErrBusy, also across api processes.
func (s *Store) StartOperation(ctx context.Context, op deploy.Operation) (deploy.Operation, error) {
	var out deploy.Operation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.InsertOperation(ctx, queries.InsertOperationParams{
			DeploymentID: op.DeploymentID, Action: string(op.Action), Graph: op.Graph, PlanDigest: optional(op.PlanDigest),
			DeleteData: op.DeleteData, RollbackOf: op.RollbackOf, PlacedSupplied: nonNil(op.PlacedSupplied),
			StartedByIss: op.StartedBy.Issuer, StartedBySub: op.StartedBy.Subject, StartedByEmail: op.StartedBy.Email,
		})
		if err != nil {
			return err
		}
		if err := q.SetLatestOperation(ctx, queries.SetLatestOperationParams{ID: op.DeploymentID, LatestOperationID: &row.ID}); err != nil {
			return err
		}
		out = operation(row)
		return nil
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
		return deploy.Operation{}, deploy.ErrBusy
	}
	return out, err
}

// DropOperation deletes a refused operation and points latest back at the one before it.
func (s *Store) DropOperation(ctx context.Context, op deploy.Operation) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ResetLatestOperation(ctx, queries.ResetLatestOperationParams{Refused: op.ID, DeploymentID: op.DeploymentID}); err != nil {
			return err
		}
		return q.DeleteOperation(ctx, op.ID)
	})
}

// AcceptOperation records the deploy target's id and boot id for a running operation.
func (s *Store) AcceptOperation(ctx context.Context, id uuid.UUID, deployerID, bootID string) error {
	return s.q.AcceptOperation(ctx, queries.AcceptOperationParams{ID: id, DeployerOperationID: &deployerID, DeployerBootID: &bootID})
}

// ProgressOperation records a running operation's step.
func (s *Store) ProgressOperation(ctx context.Context, id uuid.UUID, step deploy.Step) (bool, error) {
	n, err := s.q.ProgressOperation(ctx, queries.ProgressOperationParams{ID: id, Step: optional(string(step))})
	return n > 0, err
}

// FinishOperation gives a running operation its final status.
func (s *Store) FinishOperation(ctx context.Context, id uuid.UUID, status deploy.Status, step deploy.Step, errText string) (bool, error) {
	n, err := s.q.FinishOperation(ctx, queries.FinishOperationParams{
		ID: id, Status: string(status), Step: optional(string(step)), Error: optional(errText),
	})
	return n > 0, err
}

// Operation reads one operation.
func (s *Store) Operation(ctx context.Context, id uuid.UUID) (deploy.Operation, bool, error) {
	row, ok, err := found(s.q.GetOperation(ctx, id))
	if !ok {
		return deploy.Operation{}, false, err
	}
	return operation(row), true, nil
}

// RunningOperations lists every running operation with its deployment's name and project.
func (s *Store) RunningOperations(ctx context.Context) ([]deploy.RunningOperation, error) {
	rows, err := s.q.RunningOperations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]deploy.RunningOperation, 0, len(rows))
	for _, r := range rows {
		out = append(out, deploy.RunningOperation{Operation: operation(r.Operation), DeploymentName: r.DeploymentName, Project: r.Project})
	}
	return out, nil
}

// Deployment reads a deployment with its latest operation.
func (s *Store) Deployment(ctx context.Context, name string) (deploy.Deployment, bool, error) {
	row, ok, err := found(s.q.GetDeployment(ctx, name))
	if !ok {
		return deploy.Deployment{}, false, err
	}
	out, err := s.withLatest(ctx, []queries.Deployment{row})
	if err != nil {
		return deploy.Deployment{}, false, err
	}
	return out[0], true, nil
}

// Deployments lists deployments by name after after.
func (s *Store) Deployments(ctx context.Context, after string, limit int) ([]deploy.Deployment, error) {
	rows, err := s.q.ListDeployments(ctx, queries.ListDeploymentsParams{After: after, MaxRows: int32(min(limit, 1000))}) //nolint:gosec // bounded above
	if err != nil {
		return nil, err
	}
	return s.withLatest(ctx, rows)
}

// withLatest maps rows and reads their latest operations in one query.
func (s *Store) withLatest(ctx context.Context, rows []queries.Deployment) ([]deploy.Deployment, error) {
	var ids []uuid.UUID
	for _, r := range rows {
		if r.LatestOperationID != nil {
			ids = append(ids, *r.LatestOperationID)
		}
	}
	latest := map[uuid.UUID]deploy.Operation{}
	if len(ids) > 0 {
		ops, err := s.q.GetOperationsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, op := range ops {
			latest[op.ID] = operation(op)
		}
	}
	out := make([]deploy.Deployment, 0, len(rows))
	for _, r := range rows {
		d := deploy.Deployment{ID: r.ID, Name: r.Name, Project: r.Project, CreatedAt: r.CreatedAt}
		if r.LatestOperationID != nil {
			if op, ok := latest[*r.LatestOperationID]; ok {
				d.Latest = &op
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// Operations lists a deployment's history newest first.
func (s *Store) Operations(ctx context.Context, deploymentID uuid.UUID, before *uuid.UUID, limit int) ([]deploy.Operation, error) {
	rows, err := s.q.ListOperations(ctx, queries.ListOperationsParams{DeploymentID: deploymentID, Before: before, MaxRows: int32(min(limit, 1000))}) //nolint:gosec // bounded above
	if err != nil {
		return nil, err
	}
	out := make([]deploy.Operation, 0, len(rows))
	for _, r := range rows {
		out = append(out, operation(r))
	}
	return out, nil
}

// LatestSucceededApply returns the apply whose graph the deployment runs now.
func (s *Store) LatestSucceededApply(ctx context.Context, deploymentID uuid.UUID) (deploy.Operation, bool, error) {
	row, ok, err := found(s.q.LatestSucceededApply(ctx, deploymentID))
	if !ok {
		return deploy.Operation{}, false, err
	}
	return operation(row), true, nil
}

// LatestApplyReachedUp returns the apply whose instances the state read expects.
func (s *Store) LatestApplyReachedUp(ctx context.Context, deploymentID uuid.UUID) (deploy.Operation, bool, error) {
	row, ok, err := found(s.q.LatestApplyReachedUp(ctx, deploymentID))
	if !ok {
		return deploy.Operation{}, false, err
	}
	return operation(row), true, nil
}

func operation(r queries.Operation) deploy.Operation {
	return deploy.Operation{
		ID: r.ID, DeploymentID: r.DeploymentID, Action: deploy.Action(r.Action), Status: deploy.Status(r.Status),
		Step: deploy.Step(deref(r.Step)), Graph: r.Graph, PlanDigest: deref(r.PlanDigest), DeleteData: r.DeleteData,
		RollbackOf: r.RollbackOf, PlacedSupplied: nonNil(r.PlacedSupplied),
		StartedBy:           deploy.Starter{Issuer: r.StartedByIss, Subject: r.StartedBySub, Email: r.StartedByEmail},
		DeployerOperationID: deref(r.DeployerOperationID), DeployerBootID: deref(r.DeployerBootID),
		Error: deref(r.Error), StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // the embedded folder is fixed at build time
	}
	return sub
}
