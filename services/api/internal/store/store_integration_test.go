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
	want := []string{"admin_binding", "goose_db_version", "operations", "sessions", "stacks"}
	if !slices.Equal(tables, want) {
		t.Errorf("tables = %q, want %q", tables, want)
	}
	var indexes int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_indexes WHERE indexname IN
		('operations_one_running_idx', 'operations_stack_started_idx', 'sessions_expires_at_idx')`).Scan(&indexes); err != nil {
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
		StackID: id, Action: deploy.ActionApply, Graph: json.RawMessage(`{"name":"sample"}`), Digest: "sha256:x",
		StartedBy: deploy.Starter{Issuer: "i", Subject: "s", Email: "a@b.c"},
	}
}

func TestOperations(t *testing.T) {
	st, _ := startStore(t)
	ctx := t.Context()
	if _, _, _, err := st.EnsureStack(ctx, "modular-platform"); err == nil {
		t.Error("the reserved name was stored")
	}
	id, composeProject, created, err := st.EnsureStack(ctx, "sample")
	if err != nil || !created || composeProject != "mp-sample" {
		t.Fatalf("ensure: %v %q %v %v", id, composeProject, created, err)
	}
	if again, _, created, _ := st.EnsureStack(ctx, "sample"); again != id || created {
		t.Errorf("second ensure: %v created %v", again, created)
	}
	first, err := st.StartOperation(ctx, applyOp(id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartOperation(ctx, applyOp(id)); !errors.Is(err, deploy.ErrBusy) {
		t.Errorf("a second running operation: %v, want ErrBusy", err)
	}
	d, _, _ := st.Stack(ctx, "sample")
	if d.Latest == nil || d.Latest.ID != first.ID || d.State() != deploy.StateApplying {
		t.Errorf("stack %+v", d)
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
	d, _, _ = st.Stack(ctx, "sample")
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
	d, _, _ = st.Stack(ctx, "sample")
	if d.Latest == nil || d.Latest.ID != first.ID {
		t.Errorf("latest after a drop: %+v", d.Latest)
	}
	ops, err := st.Operations(ctx, id, nil, 10)
	if err != nil || len(ops) != 1 {
		t.Errorf("history %d rows, err %v", len(ops), err)
	}

	// a new stack refused before any operation is dropped whole
	other, _, _, _ := st.EnsureStack(ctx, "other")
	if err := st.DropNewStack(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Stack(ctx, "other"); ok {
		t.Error("the refused stack is still there")
	}
	list, err := st.Stacks(ctx, "", 10)
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
	id, _, _, _ := st.EnsureStack(ctx, "sample")
	op, err := st.StartOperation(ctx, applyOp(id))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptOperation(ctx, op.ID, "dep-1", "boot-1"); err != nil {
		t.Fatal(err)
	}
	running, err := st.RunningOperations(ctx)
	if err != nil || len(running) != 1 || running[0].DeployerBootID != "boot-1" || running[0].ComposeProject != "mp-sample" {
		t.Errorf("running %+v, err %v", running, err)
	}
}

func TestRollbackQueries(t *testing.T) {
	st, _ := startStore(t)
	ctx := t.Context()
	id, _, _, _ := st.EnsureStack(ctx, "sample")
	good, err := st.StartOperation(ctx, applyOp(id))
	if err != nil {
		t.Fatal(err)
	}
	st.FinishOperation(ctx, good.ID, deploy.StatusSucceeded, deploy.StepDone, "")
	changed := applyOp(id)
	changed.Graph = json.RawMessage(`{"name":"sample","changed":true}`)
	bad, err := st.StartOperation(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	st.FinishOperation(ctx, bad.ID, deploy.StatusFailed, deploy.StepUp, "up_failed")

	if op, ok, err := st.LatestApply(ctx, id); err != nil || !ok || op.ID != bad.ID {
		t.Errorf("latest apply %v %v %v, want the failed one", op.ID, ok, err)
	}
	if op, ok, err := st.LatestSucceededBefore(ctx, id, bad.ID); err != nil || !ok || op.ID != good.ID {
		t.Errorf("succeeded before %v %v %v, want the good one", op.ID, ok, err)
	}
	if _, ok, _ := st.LatestSucceededBefore(ctx, id, good.ID); ok {
		t.Error("found a succeeded operation before the first one")
	}

	rollback := applyOp(id)
	rollback.RollbackOf = &bad.ID
	if _, started, err := st.StartOperationIfLatest(ctx, rollback, good.ID); err != nil || started {
		t.Errorf("started while another operation is latest: %v %v", started, err)
	}
	rb, started, err := st.StartOperationIfLatest(ctx, rollback, bad.ID)
	if err != nil || !started || rb.RollbackOf == nil || *rb.RollbackOf != bad.ID {
		t.Fatalf("rollback %+v started %v err %v", rb, started, err)
	}
	if d, _, _ := st.Stack(ctx, "sample"); d.State() != deploy.StateRollingBack {
		t.Errorf("state %s, want rolling-back", d.State())
	}
	if _, started, err := st.StartOperationIfLatest(ctx, rollback, rb.ID); err != nil || started {
		t.Errorf("a second running operation started: %v %v", started, err)
	}
	if n, err := st.CountRollbacks(ctx, bad.ID); err != nil || n != 1 {
		t.Errorf("rollbacks %d, err %v", n, err)
	}
}

// TestBusyAcrossProcesses: two api processes, each with its own pool, start an operation of the
// same stack at once; exactly one gets it (AC-9).
func TestBusyAcrossProcesses(t *testing.T) {
	first, pool := startStore(t)
	otherPool, err := pgxpool.New(t.Context(), pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherPool.Close)
	second := store.New(otherPool)
	id, _, _, _ := first.EnsureStack(t.Context(), "sample")

	for round := range 10 {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		ops := make([]deploy.Operation, 2)
		for i, st := range []*store.Store{first, second} {
			wg.Go(func() { ops[i], errs[i] = st.StartOperation(t.Context(), applyOp(id)) })
		}
		wg.Wait()
		var won []deploy.Operation
		for i, err := range errs {
			switch {
			case err == nil:
				won = append(won, ops[i])
			case !errors.Is(err, deploy.ErrBusy):
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if len(won) != 1 {
			t.Fatalf("round %d: %d operations started, want 1", round, len(won))
		}
		first.FinishOperation(t.Context(), won[0].ID, deploy.StatusSucceeded, deploy.StepDone, "")
	}
}
