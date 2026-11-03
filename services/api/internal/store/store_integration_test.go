//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/signin"
	"github.com/racherom/modular-platform/services/api/internal/store"
)

// postgresImage is the pinned test image; uuidv7() needs Postgres 18.
const postgresImage = "postgres:18.6-alpine3.24"

func startStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	ctr, err := postgres.Run(t.Context(), postgresImage,
		postgres.WithDatabase("platform_api"), postgres.WithUsername("api"), postgres.WithPassword("test-password"),
		postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := ctr.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// two api processes starting at once: the session lock lets one migrate, the other waits
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = store.Migrate(t.Context(), pool) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	return store.New(pool), pool
}

func TestSchemaIsLive(t *testing.T) {
	_, pool := startStore(t)
	var tables []string
	rows, err := pool.Query(t.Context(), `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	want := []string{"admin_binding", "deployments", "goose_db_version", "operations", "sessions"}
	if !slices.Equal(tables, want) {
		t.Errorf("tables = %q, want %q", tables, want)
	}
	var indexes int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_indexes WHERE indexname IN
		('operations_one_running_idx', 'operations_deployment_started_idx', 'sessions_expires_at_idx')`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 3 {
		t.Errorf("found %d of the 3 indexes", indexes)
	}
}

func TestAdminBindsOnce(t *testing.T) {
	st, _ := startStore(t)
	ctx := t.Context()
	if _, ok, err := st.AdminBinding(ctx); err != nil || ok {
		t.Fatalf("fresh database: bound %v, err %v", ok, err)
	}
	// two first sign ins at once: exactly one identity wins, and both see the winner
	ids := []signin.Identity{{Issuer: "https://idp", Subject: "a"}, {Issuer: "https://idp", Subject: "b"}}
	got := make([]signin.Identity, 2)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			var err error
			if got[i], err = st.BindAdmin(ctx, id, "admin@example.com"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got[0] != got[1] || !slices.Contains(ids, got[0]) {
		t.Errorf("bound %v and %v", got[0], got[1])
	}
	if err := st.ResetAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.AdminBinding(ctx); ok {
		t.Error("reset left an admin bound")
	}
}

func TestSessions(t *testing.T) {
	st, pool := startStore(t)
	ctx := t.Context()
	hash := make([]byte, 32)
	hash[0] = 1
	sess := signin.Session{TokenHash: hash, Identity: signin.Identity{Issuer: "i", Subject: "s"}, Email: "a@b.c", ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.Session(ctx, hash)
	if err != nil || !ok || got.Email != "a@b.c" || got.IDToken != "" {
		t.Fatalf("session %+v, %v, %v", got, ok, err)
	}
	if err := st.CreateSession(ctx, signin.Session{TokenHash: []byte("short"), ExpiresAt: time.Now()}); err == nil {
		t.Error("a token hash of the wrong length was stored")
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() - interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	if n, err := st.DeleteStaleSessions(ctx, time.Hour); err != nil || n != 1 {
		t.Errorf("swept %d, err %v", n, err)
	}
}

func applyOp(id uuid.UUID) deploy.Operation {
	return deploy.Operation{
		DeploymentID: id, Action: deploy.ActionApply, Graph: json.RawMessage(`{"name":"sample"}`), PlanDigest: "sha256:x",
		StartedBy: deploy.Starter{Issuer: "i", Subject: "s", Email: "a@b.c"},
	}
}

func TestOperations(t *testing.T) {
	st, _ := startStore(t)
	ctx := t.Context()
	if _, _, _, err := st.EnsureDeployment(ctx, "modular-platform"); err == nil {
		t.Error("the reserved name was stored")
	}
	id, project, created, err := st.EnsureDeployment(ctx, "sample")
	if err != nil || !created || project != "mp-sample" {
		t.Fatalf("ensure: %v %q %v %v", id, project, created, err)
	}
	if again, _, created, _ := st.EnsureDeployment(ctx, "sample"); again != id || created {
		t.Errorf("second ensure: %v created %v", again, created)
	}
	first, err := st.StartOperation(ctx, applyOp(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartOperation(ctx, applyOp(id)); !errors.Is(err, deploy.ErrBusy) {
		t.Errorf("a second running operation: %v, want ErrBusy", err)
	}
	d, _, _ := st.Deployment(ctx, "sample")
	if d.Latest == nil || d.Latest.ID != first.ID || d.State() != deploy.StateApplying {
		t.Errorf("deployment %+v", d)
	}
	if changed, err := st.ProgressOperation(ctx, first.ID, deploy.StepPull); err != nil || !changed {
		t.Errorf("progress: %v %v", changed, err)
	}
	if changed, _ := st.FinishOperation(ctx, first.ID, deploy.StatusSucceeded, deploy.StepDone, ""); !changed {
		t.Error("finish changed nothing")
	}
	if changed, _ := st.FinishOperation(ctx, first.ID, deploy.StatusFailed, deploy.StepUp, "x"); changed {
		t.Error("a final status changed")
	}
	d, _, _ = st.Deployment(ctx, "sample")
	if d.State() != deploy.StateRunning {
		t.Errorf("state %s, want running", d.State())
	}

	// a refused second apply leaves no trace
	second, err := st.StartOperation(ctx, applyOp(id))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DropOperation(ctx, second); err != nil {
		t.Fatal(err)
	}
	d, _, _ = st.Deployment(ctx, "sample")
	if d.Latest == nil || d.Latest.ID != first.ID {
		t.Errorf("latest after a drop: %+v", d.Latest)
	}
	ops, err := st.Operations(ctx, id, nil, 10)
	if err != nil || len(ops) != 1 {
		t.Errorf("history %d rows, err %v", len(ops), err)
	}

	// a new deployment refused before any operation is dropped whole
	other, _, _, _ := st.EnsureDeployment(ctx, "other")
	if err := st.DropNewDeployment(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Deployment(ctx, "other"); ok {
		t.Error("the refused deployment is still there")
	}
	list, err := st.Deployments(ctx, "", 10)
	if err != nil || len(list) != 1 || list[0].Name != "sample" {
		t.Errorf("list %+v, err %v", list, err)
	}
	if _, ok, _ := st.LatestApplyReachedUp(ctx, id); !ok {
		t.Error("no apply reached up")
	}
}

func TestRunningOperations(t *testing.T) {
	st, _ := startStore(t)
	ctx := context.Background()
	id, _, _, _ := st.EnsureDeployment(ctx, "sample")
	op, err := st.StartOperation(ctx, applyOp(id))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptOperation(ctx, op.ID, "dep-1", "boot-1"); err != nil {
		t.Fatal(err)
	}
	running, err := st.RunningOperations(ctx)
	if err != nil || len(running) != 1 || running[0].DeployerBootID != "boot-1" || running[0].Project != "mp-sample" {
		t.Errorf("running %+v, err %v", running, err)
	}
}
