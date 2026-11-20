package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
)

const (
	whoamiRef   = "traefik/whoami:v1.12.0"
	postgresRef = "postgres:18.6"
	stackLabel  = "io.github.racherom.modular-platform.stack"
	sampleStack = "01960000-0000-7000-8000-000000000001"
)

func sha(c string) string { return "sha256:" + strings.Repeat(c, 64) }

// host is a fake host with whoami and postgres pulled: digest and image ID per letter.
type host struct {
	*fakeImages
}

func newHost() host {
	h := host{&fakeImages{byName: map[string]deploy.Image{}, registry: map[string]string{}}}
	h.put(whoamiRef, "traefik/whoami", "a", "1")
	h.put(postgresRef, "postgres", "b", "2")
	return h
}

// put pulls ref as the image with digest d and ID id: its tag, ref@digest and its ID name it.
func (h host) put(ref, repo, d, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	img := deploy.Image{ID: sha(id), RepoDigests: []string{repo + "@" + sha(d)}}
	if old, ok := h.byName[sha(id)]; ok {
		img.RepoDigests = append(old.RepoDigests, img.RepoDigests...)
	}
	h.byName[ref] = img
	h.byName[ref+"@"+sha(d)] = img
	h.byName[sha(id)] = img
}

// run sets the project's containers: one per service, running image ID id.
func (h host) run(images map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = nil
	for _, svc := range slices.Sorted(maps.Keys(images)) {
		ref, id, _ := strings.Cut(images[svc], "=")
		h.running = append(h.running, deploy.Container{
			ID: "c-" + svc, Image: ref, ImageID: sha(id), Status: "running",
			Labels: map[string]string{"com.docker.compose.service": svc, stackLabel: sampleStack},
		})
	}
}

// requestWith is the sample apply with the whoami and postgres images replaced.
func requestWith(whoami, postgres string) deploy.ApplyRequest {
	req := sampleRequest()
	compose := strings.Replace(sampleCompose, "image: "+whoamiRef, "image: "+whoami, 1)
	compose = strings.Replace(compose, "image: "+postgresRef, "image: "+postgres, 1)
	req.Files[0].Data = []byte(compose)
	req.Placed = nil
	return req
}

func applyOK(t *testing.T, e *deploy.Engine, req deploy.ApplyRequest) deploy.Operation {
	t.Helper()
	op, err := e.Apply(req)
	if err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, e, op.ID)
	if done.Status != deploy.StatusSucceeded {
		t.Fatalf("apply finished as %+v", done)
	}
	return done
}

func readLockFile(t *testing.T, dir, name string) (contract.ImageLock, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "mp-sample", name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	l, err := contract.DecodeImageLock(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return l, data
}

// placeSecret makes the placed secret exist, so later applies need no value.
func placeSecret(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "mp-sample", "secrets", "app")
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "uses_postgresql_password"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestApplyWritesLock covers AC-1 and AC-12: a tag with no digest records the repo digest, the
// image ID comes from the containers, both images get their keep tag, and pull uses
// --policy missing.
func TestApplyWritesLock(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
	c := &fakeCompose{}
	e, dir, logs := newEngine(t, c, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	op := applyOK(t, e, requestWith(whoamiRef, postgresRef))
	l, _ := readLockFile(t, dir, "images.lock")
	want := []contract.LockedImage{
		{Reference: postgresRef, Source: contract.ImageFromRegistry, Digest: sha("b"), ImageID: sha("2")},
		{Reference: whoamiRef, Source: contract.ImageFromRegistry, Digest: sha("a"), ImageID: sha("1")},
	}
	if !slices.Equal(l.Images, want) || l.Operation != op.ID || l.Stack != sampleStack {
		t.Errorf("lock = %+v", l)
	}
	if got := h.keepTags(); !slices.Equal(got, []string{
		"localhost/mp-keep/mp-sample:" + strings.Repeat("1", 64),
		"localhost/mp-keep/mp-sample:" + strings.Repeat("2", 64),
	}) {
		t.Errorf("keep tags = %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "mp-sample", "images.previous.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a first apply wrote images.previous.lock: %v", err)
	}
	c.mu.Lock()
	pull := c.runs[0]
	c.mu.Unlock()
	if !slices.Equal(pull[1:], []string{"pull", "--policy", "missing"}) {
		t.Errorf("pull run = %q", pull)
	}
	if !strings.Contains(logs.String(), `"msg":"images frozen"`) {
		t.Errorf("no images frozen log: %s", logs)
	}
}

// TestLockRotation covers AC-8: equal entries keep images.previous.lock, a change rotates it,
// and only the keep tags of the two locks remain.
func TestLockRotation(t *testing.T) {
	t.Parallel()
	h := newHost()
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	apply := func(whoamiDigest, whoamiID string) {
		t.Helper()
		h.put(whoamiRef, "traefik/whoami", whoamiDigest, whoamiID)
		h.run(map[string]string{"app": whoamiRef + "@" + sha(whoamiDigest) + "=" + whoamiID, "postgres": postgresRef + "@" + sha("b") + "=2"})
		applyOK(t, e, requestWith(whoamiRef+"@"+sha(whoamiDigest), postgresRef+"@"+sha("b")))
	}
	apply("a", "1")
	_, first := readLockFile(t, dir, "images.lock")
	apply("a", "1") // the same graph again
	if _, err := os.Stat(filepath.Join(dir, "mp-sample", "images.previous.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("equal entries rotated the lock: %v", err)
	}
	_, second := readLockFile(t, dir, "images.lock")
	if bytes.Equal(first, second) {
		t.Error("an equal apply did not rewrite operation and writtenAt")
	}
	apply("c", "3")
	_, prev := readLockFile(t, dir, "images.previous.lock")
	if !bytes.Equal(prev, second) {
		t.Errorf("images.previous.lock = %s, want the lock before", prev)
	}
	apply("d", "4")
	cur, _ := readLockFile(t, dir, "images.lock")
	if e, _ := cur.Find(whoamiRef); e.Digest != sha("d") {
		t.Errorf("current whoami = %+v", e)
	}
	if got := h.keepTags(); !slices.Equal(got, []string{
		"localhost/mp-keep/mp-sample:" + strings.Repeat("2", 64),
		"localhost/mp-keep/mp-sample:" + strings.Repeat("3", 64),
		"localhost/mp-keep/mp-sample:" + strings.Repeat("4", 64),
	}) {
		t.Errorf("keep tags after three generations = %v", got)
	}
}

// TestFailedApplyKeepsLock covers AC-1: a failed up leaves both files byte for byte.
func TestFailedApplyKeepsLock(t *testing.T) {
	t.Parallel()
	h := newHost()
	c := &fakeCompose{}
	e, dir, _ := newEngine(t, c, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
	applyOK(t, e, requestWith(whoamiRef, postgresRef))
	h.put(whoamiRef, "traefik/whoami", "c", "3")
	h.run(map[string]string{"app": whoamiRef + "=3", "postgres": postgresRef + "=2"})
	applyOK(t, e, requestWith(whoamiRef+"@"+sha("c"), postgresRef))
	_, lock := readLockFile(t, dir, "images.lock")
	_, prev := readLockFile(t, dir, "images.previous.lock")
	tags := h.keepTags()

	c.mu.Lock()
	c.fail = map[string]string{"up": "unhealthy"}
	c.mu.Unlock()
	h.put(whoamiRef, "traefik/whoami", "d", "4")
	op, err := e.Apply(requestWith(whoamiRef+"@"+sha("d"), postgresRef))
	if err != nil {
		t.Fatal(err)
	}
	if done := waitDone(t, e, op.ID); done.Status != deploy.StatusFailed {
		t.Fatalf("finished as %+v", done)
	}
	if _, got := readLockFile(t, dir, "images.lock"); !bytes.Equal(got, lock) {
		t.Error("a failed apply changed images.lock")
	}
	if _, got := readLockFile(t, dir, "images.previous.lock"); !bytes.Equal(got, prev) {
		t.Error("a failed apply changed images.previous.lock")
	}
	if got := h.keepTags(); !slices.Equal(got, tags) {
		t.Errorf("a failed apply changed keep tags: %v, want %v", got, tags)
	}
}

// TestLockWriteFailsSoftly covers the bookkeeping order: a keep tag failure after up leaves the
// operation succeeded, logs lock write failed with its step and writes no file.
func TestLockWriteFailsSoftly(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.tagErr = errors.New("disk full")
	h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
	e, dir, logs := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	applyOK(t, e, requestWith(whoamiRef, postgresRef))
	if _, err := os.Stat(filepath.Join(dir, "mp-sample", "images.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("images.lock written after a failed tag: %v", err)
	}
	if !strings.Contains(logs.String(), `"msg":"lock write failed"`) || !strings.Contains(logs.String(), `"step":"tag"`) {
		t.Errorf("no lock write failed log with its step: %s", logs)
	}
}

// TestBuiltImage covers AC-12's other half: an image with no repo digest is recorded as built.
func TestBuiltImage(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.mu.Lock()
	h.byName["localhost/web:dev"] = deploy.Image{ID: sha("5")}
	h.byName[sha("5")] = deploy.Image{ID: sha("5")}
	h.mu.Unlock()
	h.run(map[string]string{"app": "localhost/web:dev=5", "postgres": postgresRef + "=2"})
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	applyOK(t, e, requestWith("localhost/web:dev", postgresRef))
	l, _ := readLockFile(t, dir, "images.lock")
	if got, _ := l.Find("localhost/web:dev"); got.Source != contract.ImageBuilt || got.Digest != "" || got.ImageID != sha("5") {
		t.Errorf("built entry = %+v", got)
	}
}

// TestImagesRead covers the images read: nothing for a new project, present per entry, busy
// while an operation runs and lock_invalid for a broken file.
func TestImagesRead(t *testing.T) {
	t.Parallel()
	h := newHost()
	c := &fakeCompose{}
	e, dir, _ := newEngine(t, c, fakeDocker{images: h.fakeImages})
	ctx := context.Background()
	got, err := e.Images(ctx, "mp-sample")
	if err != nil || got.Adopted || got.Current != nil || got.Previous != nil {
		t.Fatalf("new project = %+v, %v", got, err)
	}
	placeSecret(t, dir)
	h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
	applyOK(t, e, requestWith(whoamiRef, postgresRef))
	h.mu.Lock()
	delete(h.byName, postgresRef+"@"+sha("b")) // its repo digest is gone from the host
	h.mu.Unlock()
	got, err = e.Images(ctx, "mp-sample")
	if err != nil || got.Adopted || got.Current == nil {
		t.Fatalf("after apply = %+v, %v", got, err)
	}
	if !slices.Equal(got.Current.Present, []bool{false, true}) {
		t.Errorf("present = %v, want postgres missing and whoami there", got.Current.Present)
	}

	c.block = make(chan struct{})
	op, err := e.Apply(requestWith(whoamiRef, postgresRef))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Images(ctx, "mp-sample"); !errors.Is(err, deploy.ErrBusy) {
		t.Errorf("read during an apply: %v, want ErrBusy", err)
	}
	close(c.block)
	waitDone(t, e, op.ID)

	if err := os.WriteFile(filepath.Join(dir, "mp-sample", "images.previous.lock"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = e.Images(ctx, "mp-sample")
	if invalid, ok := errors.AsType[*deploy.LockInvalidError](err); !ok || invalid.File != "images.previous.lock" {
		t.Errorf("broken file: %v, want lock_invalid naming it", err)
	}
}

// TestAdoption covers AC-9: a project with containers and no lock answers an adopted lock (an
// image with no repo digest left out), and the next apply writes it before pull.
func TestAdoption(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.mu.Lock()
	h.byName[sha("9")] = deploy.Image{ID: sha("9")} // no repo digest
	h.mu.Unlock()
	h.run(map[string]string{
		"app": whoamiRef + "=1", "postgres": postgresRef + "@" + sha("b") + "=2",
		"postgres_provision_app_db": postgresRef + "@" + sha("b") + "=2", "web": "localhost/web:dev=9",
	})
	c := &fakeCompose{}
	e, dir, _ := newEngine(t, c, fakeDocker{images: h.fakeImages})
	got, err := e.Images(context.Background(), "mp-sample")
	if err != nil || !got.Adopted || got.Current == nil {
		t.Fatalf("adopted = %+v, %v", got, err)
	}
	want := []contract.LockedImage{
		{Reference: postgresRef, Source: contract.ImageFromRegistry, Digest: sha("b"), ImageID: sha("2")},
		{Reference: whoamiRef, Source: contract.ImageFromRegistry, Digest: sha("a"), ImageID: sha("1")},
	}
	if !slices.Equal(got.Current.Lock.Images, want) || !slices.Equal(got.Current.Present, []bool{true, true}) {
		t.Errorf("adopted lock = %+v", got.Current)
	}

	placeSecret(t, dir)
	c.block = make(chan struct{})
	op, err := e.Apply(requestWith(whoamiRef, postgresRef))
	if err != nil {
		t.Fatal(err)
	}
	// prepare is done while pull blocks: the adopted lock is on disk already
	deadline := make(chan struct{})
	go func() {
		defer close(deadline)
		for {
			if l, err := os.ReadFile(filepath.Join(dir, "mp-sample", "images.lock")); err == nil && bytes.Contains(l, []byte(`"operation": "adopted"`)) {
				return
			}
		}
	}()
	<-deadline
	if tags := h.keepTags(); len(tags) != 2 {
		t.Errorf("adopted keep tags = %v", tags)
	}
	close(c.block)
	waitDone(t, e, op.ID)
}

// TestRemoveDeletesLocks covers AC-11.
func TestRemoveDeletesLocks(t *testing.T) {
	t.Parallel()
	for _, deleteData := range []bool{false, true} {
		h := newHost()
		h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
		e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
		placeSecret(t, dir)
		applyOK(t, e, requestWith(whoamiRef, postgresRef))
		h.put(whoamiRef, "traefik/whoami", "c", "3")
		h.run(map[string]string{"app": whoamiRef + "=3", "postgres": postgresRef + "=2"})
		applyOK(t, e, requestWith(whoamiRef+"@"+sha("c"), postgresRef))
		op, err := e.Remove(context.Background(), deploy.RemoveRequest{Project: "mp-sample", DeleteData: deleteData})
		if err != nil {
			t.Fatal(err)
		}
		if done := waitDone(t, e, op.ID); done.Status != deploy.StatusSucceeded {
			t.Fatalf("remove finished as %+v", done)
		}
		for _, f := range []string{"images.lock", "images.previous.lock"} {
			if _, err := os.Stat(filepath.Join(dir, "mp-sample", f)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("deleteData %v: %s still there: %v", deleteData, f, err)
			}
		}
		if tags := h.keepTags(); len(tags) != 0 {
			t.Errorf("deleteData %v: keep tags left: %v", deleteData, tags)
		}
	}
}

// TestResolve covers AC-5 on the deployer: found digests sorted by reference, an unknown tag as
// unresolved in request order with its reason.
func TestResolve(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.registry["b.test/x:1"] = sha("b")
	h.registry["a.test/x:1"] = sha("a")
	e, _, _ := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	got, err := e.Resolve(context.Background(), []string{"b.test/x:1", "c.test/x:404", "a.test/x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []contract.ImagePin{{Reference: "a.test/x:1", Digest: sha("a")}, {Reference: "b.test/x:1", Digest: sha("b")}}; !slices.Equal(got.Images, want) {
		t.Errorf("images = %+v", got.Images)
	}
	if len(got.Unresolved) != 1 || got.Unresolved[0].Index != 1 || !strings.Contains(got.Unresolved[0].Reason, "no image with this tag") {
		t.Errorf("unresolved = %+v", got.Unresolved)
	}
}

// resolveDocker is fakeDocker with a DistributionDigest the test scripts.
type resolveDocker struct {
	fakeDocker
	digest func(ctx context.Context, ref string) (string, error)
}

func (d resolveDocker) DistributionDigest(ctx context.Context, ref string) (string, error) {
	return d.digest(ctx, ref)
}

// newResolveEngine builds an engine whose registry lookups run digest, ending after timeout.
func newResolveEngine(t *testing.T, timeout time.Duration, digest func(context.Context, string) (string, error)) *deploy.Engine {
	t.Helper()
	e := deploy.New(t.Context(), deploy.Config{
		Dir: t.TempDir(), UID: os.Getuid(), GID: os.Getgid(), PullTimeout: time.Minute, WaitTimeout: 30 * time.Second,
		ResolveTimeout: timeout, ReservedPorts: []int{80, 443},
	}, &fakeCompose{}, resolveDocker{digest: digest}, slog.New(slog.DiscardHandler))
	t.Cleanup(e.Wait)
	return e
}

// TestResolveRunsFourAtOnce covers the resolve endpoint's limit (spec 0031): of eight blocked
// lookups, four run at once and never more; once released, all eight are resolved.
func TestResolveRunsFourAtOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	running, most := 0, 0
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	e := newResolveEngine(t, 10*time.Second, func(ctx context.Context, _ string) (string, error) {
		mu.Lock()
		running++
		most = max(most, running)
		if running == 4 {
			once.Do(func() { close(reached) })
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()
		select {
		case <-release:
			return sha("a"), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	refs := make([]string, 8)
	for n := range refs {
		refs[n] = fmt.Sprintf("r.test/x:%d", n)
	}
	done := make(chan deploy.Resolved)
	go func() {
		got, err := e.Resolve(t.Context(), refs)
		if err != nil {
			t.Error(err)
		}
		done <- got
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("four lookups never ran at once")
	}
	time.Sleep(50 * time.Millisecond) // a fifth lookup, if the limit were missing, would start now
	close(release)
	got := <-done
	mu.Lock()
	defer mu.Unlock()
	if most != 4 {
		t.Errorf("at most %d lookups ran at once, want 4", most)
	}
	if len(got.Images) != len(refs) || len(got.Unresolved) != 0 {
		t.Errorf("resolved %d, unresolved %+v", len(got.Images), got.Unresolved)
	}
}

// TestResolveTimeout covers MP_RESOLVE_TIMEOUT: lookups still open when it runs out are
// unresolved with reason timeout, and the ones that answered are returned.
func TestResolveTimeout(t *testing.T) {
	t.Parallel()
	e := newResolveEngine(t, 100*time.Millisecond, func(ctx context.Context, ref string) (string, error) {
		if strings.HasPrefix(ref, "fast.test/") {
			return sha("f"), nil
		}
		<-ctx.Done()
		return "", &deploy.ResolveError{Kind: deploy.ResolveOther, Err: ctx.Err()}
	})
	got, err := e.Resolve(t.Context(), []string{"slow.test/x:1", "fast.test/x:1", "slow.test/x:2"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []contract.ImagePin{{Reference: "fast.test/x:1", Digest: sha("f")}}; !slices.Equal(got.Images, want) {
		t.Errorf("images = %+v", got.Images)
	}
	if len(got.Unresolved) != 2 {
		t.Fatalf("unresolved = %+v", got.Unresolved)
	}
	for _, u := range got.Unresolved {
		if !strings.HasPrefix(u.Reason, "timeout") {
			t.Errorf("%s: reason = %q, want timeout", u.Reference, u.Reason)
		}
	}
}

// TestResolveCallerGone covers resolveReason: a caller that goes away is not reported as the
// resolve timeout.
func TestResolveCallerGone(t *testing.T) {
	t.Parallel()
	e := newResolveEngine(t, 10*time.Second, func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	got, err := e.Resolve(ctx, []string{"slow.test/x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unresolved) != 1 || strings.HasPrefix(got.Unresolved[0].Reason, "timeout") {
		t.Errorf("unresolved = %+v, want a reason other than timeout", got.Unresolved)
	}
}

// TestResolveNoEngine covers the resolve endpoint's 503: a container engine that does not answer
// fails the whole call with ErrDocker instead of listing unresolved references.
func TestResolveNoEngine(t *testing.T) {
	t.Parallel()
	e := newResolveEngine(t, 10*time.Second, func(context.Context, string) (string, error) {
		return "", &deploy.ResolveError{Kind: deploy.ResolveNoEngine, Err: errors.New("connection refused")}
	})
	if _, err := e.Resolve(t.Context(), []string{"a.test/x:1"}); !errors.Is(err, deploy.ErrDocker) {
		t.Errorf("err = %v, want ErrDocker", err)
	}
}

// TestLockServiceWithoutContainer covers AC-1's fallback: a service with no container after up
// takes its image ID from the image its reference@digest names.
func TestLockServiceWithoutContainer(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.run(map[string]string{"app": whoamiRef + "=1"}) // postgres left no container
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	applyOK(t, e, requestWith(whoamiRef, postgresRef+"@"+sha("b")))
	l, _ := readLockFile(t, dir, "images.lock")
	want := contract.LockedImage{Reference: postgresRef, Source: contract.ImageFromRegistry, Digest: sha("b"), ImageID: sha("2")}
	if got, ok := l.Find(postgresRef); !ok || got != want {
		t.Errorf("entry = %+v, want %+v", got, want)
	}
}

// TestLockContainersDisagree covers the Risks note of spec 0031: when the containers of one
// service run different image IDs (6 and 7), the entry takes the ID of the image its reference
// names (1), not either container's.
func TestLockContainersDisagree(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.mu.Lock()
	labels := map[string]string{"com.docker.compose.service": "app", stackLabel: sampleStack}
	h.running = []deploy.Container{
		{ID: "c-app-1", Image: whoamiRef, ImageID: sha("6"), Status: "running", Labels: labels},
		{ID: "c-app-2", Image: whoamiRef, ImageID: sha("7"), Status: "running", Labels: labels},
		{ID: "c-postgres", Image: postgresRef, ImageID: sha("2"), Status: "running", Labels: map[string]string{"com.docker.compose.service": "postgres", stackLabel: sampleStack}},
	}
	h.mu.Unlock()
	e, dir, _ := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	applyOK(t, e, requestWith(whoamiRef, postgresRef))
	l, _ := readLockFile(t, dir, "images.lock")
	if got, _ := l.Find(whoamiRef); got.ImageID != sha("1") || got.Digest != sha("a") {
		t.Errorf("whoami entry = %+v, want the image the tag names", got)
	}
}

// TestApplyReplacesABrokenLock covers the bookkeeping order with a broken images.lock: a
// successful apply writes its own lock, never rotates the broken file into
// images.previous.lock, and keeps only its own keep tags.
func TestApplyReplacesABrokenLock(t *testing.T) {
	t.Parallel()
	h := newHost()
	h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
	e, dir, logs := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
	placeSecret(t, dir)
	applyOK(t, e, requestWith(whoamiRef, postgresRef))
	if err := os.WriteFile(filepath.Join(dir, "mp-sample", "images.lock"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.put(whoamiRef, "traefik/whoami", "c", "3")
	h.run(map[string]string{"app": whoamiRef + "=3", "postgres": postgresRef + "=2"})
	op := applyOK(t, e, requestWith(whoamiRef+"@"+sha("c"), postgresRef))
	l, _ := readLockFile(t, dir, "images.lock")
	if got, _ := l.Find(whoamiRef); l.Operation != op.ID || got.Digest != sha("c") || got.ImageID != sha("3") {
		t.Errorf("lock = %+v", l)
	}
	if _, err := os.Stat(filepath.Join(dir, "mp-sample", "images.previous.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the broken lock was rotated into images.previous.lock: %v", err)
	}
	if got := h.keepTags(); !slices.Equal(got, []string{
		"localhost/mp-keep/mp-sample:" + strings.Repeat("2", 64),
		"localhost/mp-keep/mp-sample:" + strings.Repeat("3", 64),
	}) {
		t.Errorf("keep tags = %v, want this apply's only", got)
	}
	if !strings.Contains(logs.String(), `"msg":"lock invalid"`) || !strings.Contains(logs.String(), `"file":"images.lock"`) {
		t.Errorf("no lock invalid warning naming the file: %s", logs)
	}
}

// TestApplyDropsABrokenPreviousLock covers AC-1 and AC-8 with a broken images.previous.lock: a
// successful apply logs it as lock invalid and counts its entries as none, so the keep tags only
// it named are removed. With equal entries the broken file stays on disk (no rotation); with new
// entries the rotation overwrites it with the lock that was current.
func TestApplyDropsABrokenPreviousLock(t *testing.T) {
	t.Parallel()
	keep := func(ids ...string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, "localhost/mp-keep/mp-sample:"+strings.Repeat(id, 64))
		}
		return out
	}
	for name, tc := range map[string]struct {
		next         func(h host) deploy.ApplyRequest
		wantKeep     []string
		wantPrevious string // the digest of whoami in images.previous.lock; empty when it stays broken
	}{
		"same graph": {
			next:     func(host) deploy.ApplyRequest { return requestWith(whoamiRef+"@"+sha("c"), postgresRef) },
			wantKeep: keep("2", "3"),
		},
		"new digest": {
			next: func(h host) deploy.ApplyRequest {
				h.put(whoamiRef, "traefik/whoami", "d", "4")
				h.run(map[string]string{"app": whoamiRef + "=4", "postgres": postgresRef + "=2"})
				return requestWith(whoamiRef+"@"+sha("d"), postgresRef)
			},
			wantKeep:     keep("2", "3", "4"),
			wantPrevious: sha("c"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHost()
			h.run(map[string]string{"app": whoamiRef + "=1", "postgres": postgresRef + "=2"})
			e, dir, logs := newEngine(t, &fakeCompose{}, fakeDocker{images: h.fakeImages})
			placeSecret(t, dir)
			applyOK(t, e, requestWith(whoamiRef, postgresRef))
			h.put(whoamiRef, "traefik/whoami", "c", "3")
			h.run(map[string]string{"app": whoamiRef + "=3", "postgres": postgresRef + "=2"})
			applyOK(t, e, requestWith(whoamiRef+"@"+sha("c"), postgresRef))
			if got := h.keepTags(); !slices.Equal(got, keep("1", "2", "3")) {
				t.Fatalf("keep tags before = %v, want both generations", got)
			}
			previous := filepath.Join(dir, "mp-sample", "images.previous.lock")
			if err := os.WriteFile(previous, []byte("{"), 0o644); err != nil {
				t.Fatal(err)
			}
			applyOK(t, e, tc.next(h))
			if got := h.keepTags(); !slices.Equal(got, tc.wantKeep) {
				t.Errorf("keep tags = %v, want %v", got, tc.wantKeep)
			}
			if !strings.Contains(logs.String(), `"msg":"lock invalid"`) || !strings.Contains(logs.String(), `"file":"images.previous.lock"`) {
				t.Errorf("no lock invalid warning naming images.previous.lock: %s", logs)
			}
			if tc.wantPrevious == "" {
				if data, err := os.ReadFile(previous); err != nil || string(data) != "{" {
					t.Errorf("images.previous.lock = %q, %v; want the broken file left as it was", data, err)
				}
				return
			}
			l, _ := readLockFile(t, dir, "images.previous.lock")
			if got, _ := l.Find(whoamiRef); got.Digest != tc.wantPrevious {
				t.Errorf("previous whoami = %+v, want digest %s", got, tc.wantPrevious)
			}
		})
	}
}
