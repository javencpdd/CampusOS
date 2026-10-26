package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/plugin"
)

func TestV5ContainerImageMetadataIdentityAndPlatform(t *testing.T) {
	ref := "registry.example/plugin@sha256:" + strings.Repeat("a", 64)
	x := imageInspection{ID: "sha256:" + strings.Repeat("b", 64), RepoDigests: []string{ref}, OS: "linux", Architecture: "amd64"}
	raw, _ := json.Marshal(x)
	got, err := validateImageInspection(ref, raw, "amd64")
	if err != nil || got.ID != x.ID {
		t.Fatal("manifest digest incorrectly compared to config ID", err)
	}
	cases := []struct {
		name   string
		change func(*imageInspection)
		err    error
	}{
		{"wrong-repository", func(x *imageInspection) {
			x.RepoDigests = []string{"elsewhere.example/plugin@sha256:" + strings.Repeat("a", 64)}
		}, ErrImageIdentity},
		{"wrong-digest", func(x *imageInspection) {
			x.RepoDigests = []string{"registry.example/plugin@sha256:" + strings.Repeat("c", 64)}
		}, ErrImageIdentity},
		{"unverified-config-id", func(x *imageInspection) { x.RepoDigests = nil }, ErrImageIdentity},
		{"malformed-id", func(x *imageInspection) { x.ID = "worker:latest" }, ErrImageIdentity},
		{"windows-image", func(x *imageInspection) { x.OS = "windows" }, ErrImagePlatform},
		{"foreign-arch", func(x *imageInspection) { x.Architecture = "arm64" }, ErrImagePlatform},
		{"image-volume-creates-ambient-storage", func(x *imageInspection) { x.Volumes = map[string]any{"/data": map[string]any{}} }, ErrImagePlatform},
		{"metadata-budget", func(x *imageInspection) { x.RepoDigests = make([]string, 65); x.RepoDigests[0] = ref }, ErrImageIdentity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := x
			c.change(&bad)
			raw, _ := json.Marshal(bad)
			if _, err := validateImageInspection(ref, raw, "amd64"); !errors.Is(err, c.err) {
				t.Fatal(err)
			}
		})
	}
	for _, bad := range [][]byte{append(bytes.Clone(raw), []byte(` {}`)...), []byte(`null`), []byte(`[]`), []byte(`{"id":null}`), bytes.Repeat([]byte(" "), imageInspectLimit+1)} {
		if _, err := validateImageInspection(ref, bad, "amd64"); err == nil {
			t.Fatal("malformed/unbounded metadata accepted")
		}
	}
	unknown := append(bytes.Clone(raw[:len(raw)-1]), []byte(`,"secret":"must-not-be-projected"}`)...)
	if _, err := validateImageInspection(ref, unknown, "amd64"); err != ErrImageIdentity {
		t.Fatal("unexpected fields accepted", err)
	}
}

func TestV5ContainerImageAdmissionBudgetsAndZero(t *testing.T) {
	var out boundedImageOutput
	if _, err := out.Write(make([]byte, imageInspectLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte{0}); err == nil || !out.overflow || out.Len() != imageInspectLimit {
		t.Fatal("stdout limit did not stop writer")
	}
	// io.Copy must not bypass the limit via an embedded Buffer.ReadFrom.
	var streamed boundedImageOutput
	_, err := io.Copy(&streamed, io.LimitReader(strings.NewReader(strings.Repeat("x", imageInspectLimit+1)), imageInspectLimit+1))
	if err == nil || !streamed.overflow || streamed.Len() > imageInspectLimit {
		t.Fatal("streaming stdout bypassed admission budget")
	}
	if got, err := ResolveLocalImage(context.Background(), plugin.V5ContainerProgram{}); err != ErrImageIdentity || got.ID() != "" {
		t.Fatal("zero program accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveLocalImage(ctx, plugin.V5ContainerProgram{}); err != context.Canceled {
		t.Fatal("canceled admission", err)
	}
}
