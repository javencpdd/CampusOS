// Package container provides bounded Container artifact admission and execution.
package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"runtime"
	"slices"
	"time"

	"github.com/campusos/CampusOS/internal/plugin"
)

var (
	ErrImageUnavailable = errors.New("plugin.container_image_unavailable")
	ErrImageIdentity    = errors.New("plugin.container_image_identity_invalid")
	ErrImagePlatform    = errors.New("plugin.container_image_platform_unsupported")
)

const imageInspectLimit = 64 << 10

var imageIDPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Image is a local immutable daemon mapping, not a readiness/authority proof.
// OCI manifest/RepoDigest and platform configuration ID are distinct namespaces.
type Image struct {
	reference, id, packageDigest, transport, architecture string
	coordinate                                            plugin.V5Coordinate
}

func (i Image) Reference() string               { return i.reference }
func (i Image) ID() string                      { return i.id }
func (i Image) PackageDigest() string           { return i.packageDigest }
func (i Image) Coordinate() plugin.V5Coordinate { return i.coordinate }
func (i Image) Transport() string               { return i.transport }
func (i Image) Architecture() string            { return i.architecture }

type imageInspection struct {
	ID           string         `json:"id"`
	RepoDigests  []string       `json:"repo_digests"`
	OS           string         `json:"os"`
	Architecture string         `json:"architecture"`
	Volumes      map[string]any `json:"volumes"`
}

func validateImageInspection(ref string, raw []byte, architecture string) (imageInspection, error) {
	var x imageInspection
	if len(raw) > imageInspectLimit {
		return x, ErrImageIdentity
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&x) != nil || d.Decode(new(any)) != io.EOF || !imageIDPattern.MatchString(x.ID) || len(x.RepoDigests) > 64 || !slices.Contains(x.RepoDigests, ref) {
		return imageInspection{}, ErrImageIdentity
	}
	if x.OS != "linux" || x.Architecture != architecture || len(x.Volumes) != 0 {
		return imageInspection{}, ErrImagePlatform
	}
	return x, nil
}

type boundedImageOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *boundedImageOutput) Len() int      { return b.buffer.Len() }
func (b *boundedImageOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *boundedImageOutput) Write(p []byte) (int, error) {
	if len(p) > imageInspectLimit-b.Len() {
		b.overflow = true
		return 0, io.ErrShortBuffer
	}
	return b.buffer.Write(p)
}

// ResolveLocalImage only inspects an already cached exact signed RepoDigest.
// No tags, registry lookup/pull, installation, container creation or listener.
// A temporary empty CLI configuration and explicit local daemon exclude inherited
// Docker contexts, registry auth, proxies and application environment variables.
func ResolveLocalImage(ctx context.Context, p plugin.V5ContainerProgram) (Image, error) {
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	if p.PackageDigest() == "" || p.ImageReference() == "" {
		return Image{}, ErrImageIdentity
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return Image{}, ErrImagePlatform
	}
	cliHome, err := os.MkdirTemp("", "campusos-container-inspect-")
	if err != nil {
		return Image{}, ErrImageUnavailable
	}
	defer os.RemoveAll(cliHome)
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	format := `{"id":{{json .Id}},"repo_digests":{{json .RepoDigests}},"os":{{json .Os}},"architecture":{{json .Architecture}},"volumes":{{json (index .Config "Volumes")}}}`
	command := localCommand(bounded, cliHome, "image", "inspect", "--format="+format, "--", p.ImageReference())
	command.WaitDelay = time.Second
	var out boundedImageOutput
	command.Stdout = &out
	command.Stderr = io.Discard
	if err := command.Run(); err != nil || out.overflow {
		if ctx.Err() != nil {
			return Image{}, ctx.Err()
		}
		return Image{}, ErrImageUnavailable
	}
	x, err := validateImageInspection(p.ImageReference(), out.Bytes(), runtime.GOARCH)
	if ctx.Err() != nil {
		return Image{}, ctx.Err()
	}
	if err != nil {
		return Image{}, err
	}
	return Image{reference: p.ImageReference(), id: x.ID, packageDigest: p.PackageDigest(), transport: p.Transport(), architecture: x.Architecture, coordinate: p.Coordinate()}, nil
}
