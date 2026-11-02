//go:build integration

package sdk_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/racherom/modular-platform/sdk"
	"github.com/racherom/modular-platform/sdk/internal/testmodule"
)

// Test images, pinned (spec 0005, "Build plan"). MinIO publishes no public image any more, so the
// S3 tests run Chainguard's maintained build, pinned by digest because only its latest tag is free.
const (
	postgresImage = "postgres:18.6-alpine3.24"
	minioImage    = "cgr.dev/chainguard/minio@sha256:3b6e40c30ecd5cea627e382ff0b82ca841f58c52294578787043ee80c8ef7c82"
)

// requiring returns the test module's declaration with the binding to capability required, so
// readiness waits for it.
func requiring(capability string) sdk.Declaration[testmodule.Config, testmodule.Uses] {
	d := testmodule.Module
	d.Bindings = slices.Clone(d.Bindings)
	for i := range d.Bindings {
		if d.Bindings[i].Capability == capability {
			d.Bindings[i].Optional = false
		}
	}
	return d
}

// buildData builds d as a consumer with env added to baseEnv and returns its handler, its Uses
// and its logs.
func buildData(t *testing.T, d sdk.Declaration[testmodule.Config, testmodule.Uses], env map[string]string) (http.Handler, testmodule.Uses, *syncBuffer) {
	t.Helper()
	base := baseEnv(t)
	maps.Copy(base, env)
	var (
		logs syncBuffer
		uses testmodule.Uses
	)
	h, err := sdk.Build(t.Context(), d, func(_ context.Context, m *tmModule) (http.Handler, error) {
		uses = m.Uses
		return http.NotFoundHandler(), nil
	}, sdk.WithEnv(base), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs.String())
	}
	return h, uses, &logs
}

func startPostgres(t *testing.T) (url string) {
	t.Helper()
	ctr, err := postgres.Run(t.Context(), postgresImage,
		postgres.WithDatabase("app"),
		postgres.WithUsername("app"),
		postgres.WithPassword(dbPassword),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := ctr.Endpoint(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	return "postgres://" + endpoint
}

// TestPostgreSQLIntegration covers the PostgreSQL part of AC-6 and AC-9: the generated getter's
// pool signs in with the binding's user, password file and database, and /readyz turns 200 once
// the database answers a ping.
func TestPostgreSQLIntegration(t *testing.T) {
	t.Parallel()
	url := startPostgres(t)
	h, uses, logs := buildData(t, requiring("postgresql"), dataEnv(map[string]string{"TM_DB_URL": url})(t))
	eventually(t, "/readyz answers 200", func() bool { return readyz(t, h) == http.StatusOK })

	pool, err := uses.Postgresql(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var user, database string
	if err := pool.QueryRow(t.Context(), "select current_user, current_database()").Scan(&user, &database); err != nil {
		t.Fatal(err)
	}
	if user != "app" || database != "app" {
		t.Errorf("signed in as %s to %s, want app to app", user, database)
	}
	if strings.Contains(logs.String(), dbPassword) {
		t.Error("the password shows in the logs")
	}
}

// TestPostgreSQLSignInFails covers AC-9's sign in failure: a wrong password keeps /readyz at 503
// and logs a warning naming the capability, never the password.
func TestPostgreSQLSignInFails(t *testing.T) {
	t.Parallel()
	url := startPostgres(t)
	const wrong = "wrong-password-0123456789"
	h, _, logs := buildData(t, requiring("postgresql"), dataEnv(map[string]string{"TM_DB_URL": url, "TM_DB_PASSWORD_FILE": file(t, wrong)})(t))
	eventually(t, "a binding not ready warning", func() bool {
		return strings.Contains(logs.String(), `"msg":"binding not ready","capability":"postgresql"`)
	})
	if code := readyz(t, h); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz = %d, want 503", code)
	}
	if strings.Contains(logs.String(), wrong) {
		t.Error("the password shows in the logs")
	}
}

// TestS3Integration covers the S3 part of AC-6 and AC-9: the generated getter's client signs with
// the binding's access key and secret key file and carries the bucket, and /readyz stays 503 until
// the bucket exists.
func TestS3Integration(t *testing.T) {
	t.Parallel()
	ctr, err := tcminio.Run(t.Context(), minioImage, tcminio.WithUsername("access"), tcminio.WithPassword(s3Secret))
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := ctr.ConnectionString(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	h, uses, logs := buildData(t, requiring("storage"), dataEnv(map[string]string{"TM_STORE_URL": "http://" + endpoint})(t))
	eventually(t, "a binding not ready warning", func() bool {
		return strings.Contains(logs.String(), `"msg":"binding not ready","capability":"storage"`)
	})
	if code := readyz(t, h); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz before the bucket exists = %d, want 503", code)
	}

	store, err := uses.Storage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MakeBucket(t.Context(), store.Bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "/readyz answers 200", func() bool { return readyz(t, h) == http.StatusOK })

	body := []byte("hello")
	if _, err := store.PutObject(t.Context(), store.Bucket, "greeting", bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	obj, err := store.GetObject(t.Context(), store.Bucket, "greeting", minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	got, err := io.ReadAll(obj)
	if err != nil || string(got) != "hello" {
		t.Errorf("read back %q, %v", got, err)
	}
	if strings.Contains(logs.String(), s3Secret) {
		t.Error("the secret key shows in the logs")
	}
}
