package contract_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/racherom/modular-platform/libs/contract"
)

const lockStack = "0193f0c2-7a4e-7cc1-9b1e-2f3a4b5c6d7e"

func hexOf(c string) string { return "sha256:" + strings.Repeat(c, 64) }

func registry(ref, d, id string) contract.LockedImage {
	return contract.LockedImage{Reference: ref, Source: contract.ImageFromRegistry, Digest: hexOf(d), ImageID: hexOf(id)}
}

func TestNewImageLock(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 13, 11, 0, 0, 0, time.FixedZone("x", 3600))
	l, err := contract.NewImageLock(lockStack, "op-1", at, []contract.LockedImage{
		registry("redis:8.2", "b", "c"),
		registry("postgres:18.6", "a", "d"),
		{Reference: "localhost/mp/web:dev", Source: contract.ImageBuilt, ImageID: hexOf("e")},
	})
	if err != nil {
		t.Fatalf("NewImageLock: %v", err)
	}
	if got := []string{l.Images[0].Reference, l.Images[1].Reference, l.Images[2].Reference}; !reflect.DeepEqual(got, []string{"localhost/mp/web:dev", "postgres:18.6", "redis:8.2"}) {
		t.Errorf("order = %v", got)
	}
	if l.WrittenAt.Location() != time.UTC || l.FormatVersion != 1 {
		t.Errorf("writtenAt %v, formatVersion %d", l.WrittenAt, l.FormatVersion)
	}
	data, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := contract.DecodeImageLock(data)
	if err != nil {
		t.Fatalf("decode own output: %v", err)
	}
	if !reflect.DeepEqual(back, l) {
		t.Errorf("round trip = %+v, want %+v", back, l)
	}
	if e, ok := l.Find("postgres:18.6"); !ok || e.Digest != hexOf("a") {
		t.Errorf("Find = %+v, %v", e, ok)
	}
	if _, ok := l.Find("nginx:1"); ok {
		t.Error("Find of a missing reference succeeded")
	}
}

func TestImageLockRules(t *testing.T) {
	t.Parallel()
	at := time.Unix(1, 0)
	good := registry("postgres:18.6", "a", "b")
	tests := map[string]struct {
		stack, op string
		at        time.Time
		images    []contract.LockedImage
	}{
		"no images":       {lockStack, "op", at, nil},
		"bad stack":       {"mp-x", "op", at, []contract.LockedImage{good}},
		"no operation":    {lockStack, "", at, []contract.LockedImage{good}},
		"no time":         {lockStack, "op", time.Time{}, []contract.LockedImage{good}},
		"twice":           {lockStack, "op", at, []contract.LockedImage{good, good}},
		"untagged":        {lockStack, "op", at, []contract.LockedImage{registry("postgres", "a", "b")}},
		"digest in ref":   {lockStack, "op", at, []contract.LockedImage{registry("postgres:18.6@"+hexOf("a"), "a", "b")}},
		"short digest":    {lockStack, "op", at, []contract.LockedImage{{Reference: "p:1", Source: contract.ImageFromRegistry, Digest: "sha256:ab", ImageID: hexOf("b")}}},
		"upper digest":    {lockStack, "op", at, []contract.LockedImage{registry("p:1", "A", "b")}},
		"no digest":       {lockStack, "op", at, []contract.LockedImage{{Reference: "p:1", Source: contract.ImageFromRegistry, ImageID: hexOf("b")}}},
		"built digest":    {lockStack, "op", at, []contract.LockedImage{{Reference: "p:1", Source: contract.ImageBuilt, Digest: hexOf("a"), ImageID: hexOf("b")}}},
		"bad source":      {lockStack, "op", at, []contract.LockedImage{{Reference: "p:1", Source: "other", ImageID: hexOf("b")}}},
		"bad image id":    {lockStack, "op", at, []contract.LockedImage{{Reference: "p:1", Source: contract.ImageBuilt, ImageID: "abc"}}},
		"long reference":  {lockStack, "op", at, []contract.LockedImage{registry("r/"+strings.Repeat("a", 260)+":1", "a", "b")}},
		"uppercase stack": {strings.ToUpper(lockStack), "op", at, []contract.LockedImage{good}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := contract.NewImageLock(tt.stack, tt.op, tt.at, tt.images); !errors.Is(err, contract.ErrInvalidLock) {
				t.Errorf("err = %v, want ErrInvalidLock", err)
			}
		})
	}
}

func TestDecodeImageLock(t *testing.T) {
	t.Parallel()
	entry := `{"reference":"postgres:18.6","source":"registry","digest":"` + hexOf("a") + `","imageId":"` + hexOf("b") + `"}`
	entry2 := `{"reference":"redis:8","source":"registry","digest":"` + hexOf("a") + `","imageId":"` + hexOf("b") + `"}`
	head := `{"formatVersion":1,"stack":"` + lockStack + `","operation":"op","writtenAt":"2026-11-13T10:00:00Z","images":[`
	tests := map[string]struct {
		data string
		ok   bool
	}{
		"good":           {head + entry + `]}`, true},
		"unknown field":  {head + entry + `],"extra":1}`, false},
		"version 2":      {strings.Replace(head, `"formatVersion":1`, `"formatVersion":2`, 1) + entry + `]}`, false},
		"unsorted":       {head + entry2 + "," + entry + `]}`, false},
		"trailing data":  {head + entry + `]} {}`, false},
		"not json":       {"nope", false},
		"entry unknown":  {head + strings.Replace(entry, `"reference"`, `"x":1,"reference"`, 1) + `]}`, false},
		"empty images":   {head + `]}`, false},
		"two entries ok": {head + entry + "," + entry2 + `]}`, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := contract.DecodeImageLock([]byte(tt.data))
			if tt.ok != (err == nil) {
				t.Errorf("err = %v, want ok %v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, contract.ErrInvalidLock) {
				t.Errorf("err = %v, want ErrInvalidLock", err)
			}
		})
	}
}

func TestSameImages(t *testing.T) {
	t.Parallel()
	a, _ := contract.NewImageLock(lockStack, "op-1", time.Unix(1, 0), []contract.LockedImage{registry("p:1", "a", "b")})
	b, _ := contract.NewImageLock(lockStack, "op-2", time.Unix(2, 0), []contract.LockedImage{registry("p:1", "a", "b")})
	c, _ := contract.NewImageLock(lockStack, "op-3", time.Unix(3, 0), []contract.LockedImage{registry("p:1", "a", "c")})
	if !a.SameImages(b) {
		t.Error("equal entries from other operations are not the same")
	}
	if a.SameImages(c) {
		t.Error("another image ID counts as the same")
	}
}

func TestImagePinProblems(t *testing.T) {
	t.Parallel()
	pins := []contract.ImagePin{
		{Reference: "postgres:18.6", Digest: hexOf("a")},
		{Reference: "postgres", Digest: hexOf("a")},
		{Reference: "redis:8", Digest: "sha256:abc"},
		{Reference: "postgres:18.6", Digest: hexOf("b")},
	}
	got := contract.ImagePinProblems(pins)
	var paths []string
	for _, p := range got {
		if p.Code != contract.CodeInvalidLock || p.Severity != contract.Error {
			t.Errorf("problem %+v", p)
		}
		paths = append(paths, p.Path)
	}
	if want := []string{"lock[1]", "lock[2]", "lock[3]"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	if got := contract.ImagePinProblems(pins[:1]); len(got) != 0 {
		t.Errorf("good lock: %v", got)
	}
}

func TestFreeze(t *testing.T) {
	t.Parallel()
	current := []contract.LockedImage{
		registry("app:1", "a", "1"),
		registry("db:1", "b", "2"),
		{Reference: "web:dev", Source: contract.ImageBuilt, ImageID: hexOf("3")},
	}
	tests := map[string]struct {
		refs, update []string
		current      []contract.LockedImage
		resolved     map[string]string
		toResolve    []string
		want         []contract.FrozenImage
		missing      []string
	}{
		"all frozen, no registry": {
			refs: []string{"db:1", "app:1", "app:1"}, current: current,
			want: []contract.FrozenImage{
				{Reference: "app:1", Digest: hexOf("a"), Change: contract.ChangeFrozen},
				{Reference: "db:1", Digest: hexOf("b"), Change: contract.ChangeFrozen},
			},
		},
		"new reference": {
			refs: []string{"app:1", "cache:2"}, current: current,
			resolved: map[string]string{"cache:2": hexOf("c")}, toResolve: []string{"cache:2"},
			want: []contract.FrozenImage{
				{Reference: "app:1", Digest: hexOf("a"), Change: contract.ChangeFrozen},
				{Reference: "cache:2", Digest: hexOf("c"), Change: contract.ChangeNew},
			},
		},
		"update takes the registry digest": {
			refs: []string{"app:1"}, current: current, update: []string{"app:1"},
			resolved: map[string]string{"app:1": hexOf("d")}, toResolve: []string{"app:1"},
			want: []contract.FrozenImage{{Reference: "app:1", Digest: hexOf("d"), Change: contract.ChangeUpdated, PreviousDigest: hexOf("a")}},
		},
		"update with the same digest is still updated": {
			refs: []string{"app:1"}, current: current, update: []string{"app:1"},
			resolved: map[string]string{"app:1": hexOf("a")}, toResolve: []string{"app:1"},
			want: []contract.FrozenImage{{Reference: "app:1", Digest: hexOf("a"), Change: contract.ChangeUpdated, PreviousDigest: hexOf("a")}},
		},
		"built entry is resolved as new": {
			refs: []string{"web:dev"}, current: current,
			resolved: map[string]string{"web:dev": hexOf("e")}, toResolve: []string{"web:dev"},
			want: []contract.FrozenImage{{Reference: "web:dev", Digest: hexOf("e"), Change: contract.ChangeNew}},
		},
		"no current lock": {
			refs:     []string{"app:1"},
			resolved: map[string]string{"app:1": hexOf("f"), "unused:1": hexOf("0")}, toResolve: []string{"app:1"},
			want: []contract.FrozenImage{{Reference: "app:1", Digest: hexOf("f"), Change: contract.ChangeNew}},
		},
		"unresolved": {
			refs: []string{"app:1", "x:1", "cache:2"}, current: current,
			toResolve: []string{"cache:2", "x:1"}, missing: []string{"cache:2", "x:1"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := contract.ToResolve(tt.refs, tt.current, tt.update); !reflect.DeepEqual(got, tt.toResolve) {
				t.Errorf("ToResolve = %v, want %v", got, tt.toResolve)
			}
			got, err := contract.Freeze(tt.refs, tt.current, tt.resolved, tt.update)
			if tt.missing != nil {
				var u *contract.UnresolvedError
				if !errors.As(err, &u) || !reflect.DeepEqual(u.References, tt.missing) {
					t.Fatalf("err = %v, want unresolved %v", err, tt.missing)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Freeze = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReviewDigest(t *testing.T) {
	t.Parallel()
	a := []contract.ImagePin{{Reference: "b:1", Digest: hexOf("b")}, {Reference: "a:1", Digest: hexOf("a")}}
	b := []contract.ImagePin{{Reference: "a:1", Digest: hexOf("a")}, {Reference: "b:1", Digest: hexOf("b")}}
	d := contract.ReviewDigest("sha256:x", a)
	if d != contract.ReviewDigest("sha256:x", b) {
		t.Error("order changes the digest")
	}
	if !contract.ValidImageDigest(d) {
		t.Errorf("digest %q is not sha256 and 64 hex", d)
	}
	if d == contract.ReviewDigest("sha256:y", a) {
		t.Error("check digest not covered")
	}
	if d == contract.ReviewDigest("sha256:x", []contract.ImagePin{{Reference: "a:1", Digest: hexOf("a")}, {Reference: "b:1", Digest: hexOf("c")}}) {
		t.Error("an image digest not covered")
	}
	if a[0].Reference != "b:1" {
		t.Error("ReviewDigest sorted its input in place")
	}
}
