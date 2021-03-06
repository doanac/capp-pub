package internal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	compose "github.com/compose-spec/compose-go/types"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/builder/dockerignore"
	"github.com/opencontainers/go-digest"
)

func getContainerConfig(mansvc distribution.ManifestService, blobStore distribution.BlobStore, ctx context.Context, configDigest digest.Digest) ([]byte, error) {
	mm, e := mansvc.Get(ctx, configDigest)
	if e != nil {
		return nil, e
	}
	dgst := mm.(*schema2.DeserializedManifest).Manifest.Config.Digest
	return blobStore.Get(ctx, dgst)
}

func iterateServices(services map[string]interface{}, proj *compose.Project, fn compose.ServiceFunc) error {
	return proj.WithServices(nil, func(s compose.ServiceConfig) error {
		obj := services[s.Name]
		_, ok := obj.(map[string]interface{})
		if !ok {
			if s.Name == "extensions" {
				fmt.Println("Hacking around https://github.com/compose-spec/compose-go/issues/91")
				return nil
			}
			return fmt.Errorf("Service(%s) has invalid format", s.Name)
		}
		return fn(s)
	})
}

type ContainerConfig struct {
	Platform string
	Digest   digest.Digest
	Config   []byte
}
type ServiceConfigs map[string][]ContainerConfig

func PinServiceImages(ctx context.Context, services map[string]interface{}, proj *compose.Project) (ServiceConfigs, error) {
	regc := NewRegistryClient()

	configs := make(ServiceConfigs)

	return configs, iterateServices(services, proj, func(s compose.ServiceConfig) error {
		name := s.Name
		obj := services[name]
		svc, ok := obj.(map[string]interface{})

		image := s.Image
		if len(image) == 0 {
			return fmt.Errorf("Service(%s) missing 'image' attribute", name)
		}

		fmt.Printf("Pinning %s(%s)\n", name, image)
		named, err := reference.ParseNormalizedNamed(image)
		if err != nil {
			return err
		}

		repo, err := regc.GetRepository(ctx, named)
		if err != nil {
			return err
		}
		namedTagged, ok := named.(reference.Tagged)
		if !ok {
			return fmt.Errorf("Invalid image reference(%s): Images must be tagged. e.g %s:stable", image, image)
		}
		tag := namedTagged.Tag()
		desc, err := repo.Tags(ctx).Get(ctx, tag)
		if err != nil {
			return fmt.Errorf("Unable to find image reference(%s): %s", image, err)
		}
		mansvc, err := repo.Manifests(ctx, nil)
		if err != nil {
			return fmt.Errorf("Unable to get image manifests(%s): %s", image, err)
		}
		man, err := mansvc.Get(ctx, desc.Digest)
		if err != nil {
			return fmt.Errorf("Unable to find image manifest(%s): %s", image, err)
		}

		blobStore := repo.Blobs(ctx)

		// TODO - we should find the intersection of platforms so
		// that we can denote the platforms this app can run on
		pinned := reference.Domain(named) + "/" + reference.Path(named) + "@" + desc.Digest.String()

		switch mani := man.(type) {
		case *manifestlist.DeserializedManifestList:
			containerConfigs := make([]ContainerConfig, len(mani.Manifests))
			fmt.Printf("  | ")
			for i, m := range mani.Manifests {
				if i != 0 {
					fmt.Printf(", ")
				}
				plat := m.Platform.Architecture
				if m.Platform.Architecture == "arm" {
					plat += m.Platform.Variant
				}
				fmt.Printf(plat)
				cfg, e := getContainerConfig(mansvc, blobStore, ctx, m.Descriptor.Digest)
				if e != nil {
					return fmt.Errorf("Unable to container config for %s: %v", plat, e)
				}
				containerConfigs[i] = ContainerConfig{Platform: plat, Config: cfg, Digest: m.Digest}
			}
			configs[name] = containerConfigs
		case *schema2.DeserializedManifest:
			cfg, e := getContainerConfig(mansvc, blobStore, ctx, desc.Digest)
			if e != nil {
				return fmt.Errorf("Unable to container config: %v", e)
			}
			configs[name] = []ContainerConfig{
				{Config: cfg, Digest: desc.Digest},
			}
			break
		default:
			return fmt.Errorf("Unexpected manifest: %v", mani)
		}

		fmt.Println("\n  |-> ", pinned)
		svc["image"] = pinned
		return nil
	})
}

func getIgnores(appDir string) []string {
	file, err := os.Open(filepath.Join(appDir, ".composeappignores"))
	if err != nil {
		return nil
	}
	ignores, _ := dockerignore.ReadAll(file)
	file.Close()
	if ignores != nil {
		ignores = append(ignores, ".composeappignores")
	}
	return ignores
}

func createTgz(composeContent []byte, appDir string, ostreeShas, specFiles map[string][]byte, unitFiles map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	ignores := getIgnores(appDir)
	warned := make(map[string]bool)

	for name, content := range ostreeShas {
		header := tar.Header{
			Name: ".ostree/" + name,
			Size: int64(len(content)),
			Mode: 0755,
		}
		if err := tw.WriteHeader(&header); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}

	for name, content := range specFiles {
		header := tar.Header{
			Name: ".specs/" + name,
			Size: int64(len(content)),
			Mode: 0755,
		}
		if err := tw.WriteHeader(&header); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}

	for name, content := range unitFiles {
		header := tar.Header{
			Name: ".systemd/" + name,
			Size: int64(len(content)),
			Mode: 0755,
		}
		if err := tw.WriteHeader(&header); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}

	header := tar.Header{
		Name: "docker-compose.json",
		Size: int64(len(composeContent)),
		Mode: 0755,
	}
	if err := tw.WriteHeader(&header); err != nil {
		return nil, err
	}
	if _, err := tw.Write(composeContent); err != nil {
		return nil, fmt.Errorf("Unable to add docker-compose.json to archive: %s", err)
	}

	err := filepath.Walk(appDir, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("Tar: Can't stat file %s to tar: %w", appDir, err)
		}

		if fi.Mode().IsDir() {
			return nil
		}
		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}
		if fi.Name() == "docker-compose.yml" {
			return nil
		}

		// Handle subdirectories
		header.Name = strings.TrimPrefix(strings.Replace(file, appDir, "", -1), string(filepath.Separator))
		if ignores != nil {
			for _, ignore := range ignores {
				if match, err := filepath.Match(ignore, header.Name); err == nil && match {
					if !warned[ignore] {
						fmt.Println("  |-> ignoring: ", ignore)
					}
					warned[ignore] = true
					return nil
				}
			}
		}

		if !fi.Mode().IsRegular() {
			if fi.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(header.Name)
				if err != nil {
					return fmt.Errorf("Tar: Can't find symlink: %s", err)
				}
				header.Linkname = link
			} else {
				// TODO handle the different types similar to
				// https://github.com/moby/moby/blob/master/pkg/archive/archive.go#L573
				return fmt.Errorf("Tar: Can't tar non regular types yet: %s", header.Name)
			}
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if fi.Mode().IsRegular() {
			f, err := os.Open(file)
			if err != nil {
				f.Close()
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			f.Close()
		}

		return nil
	})

	tw.Close()
	gzw.Close()

	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func CreateApp(ctx context.Context, config map[string]interface{}, target string, ostreeShas, specFiles map[string][]byte, unitFiles map[string][]byte, dryRun bool) (string, error) {
	pinned, err := json.Marshal(config)
	if err != nil {
		return "", err
	}

	buff, err := createTgz(pinned, "./", ostreeShas, specFiles, unitFiles)
	if err != nil {
		return "", err
	}

	named, err := reference.ParseNormalizedNamed(target)
	if err != nil {
		return "", err
	}
	tag := "latest"
	if tagged, ok := reference.TagNameOnly(named).(reference.Tagged); ok {
		tag = tagged.Tag()
	}

	regc := NewRegistryClient()
	repo, err := regc.GetRepository(ctx, named)
	if err != nil {
		return "", err
	}

	if dryRun {
		fmt.Println("Pinned compose:")
		fmt.Println(string(pinned))
		fmt.Println("Skipping publishing for dryrun")
		if err := ioutil.WriteFile("compose-bundle.tgz", buff, 0755); err != nil {
			return "", err
		}
		return "", nil
	}

	blobStore := repo.Blobs(ctx)
	desc, err := blobStore.Put(ctx, "application/tar+gzip", buff)
	if err != nil {
		return "", err
	}
	fmt.Println("  |-> app: ", desc.Digest.String())

	mb := ocischema.NewManifestBuilder(blobStore, []byte{}, map[string]string{"compose-app": "v1"})
	if err := mb.AppendReference(desc); err != nil {
		return "", err
	}

	manifest, err := mb.Build(ctx)
	if err != nil {
		return "", err
	}
	svc, err := repo.Manifests(ctx, nil)
	if err != nil {
		return "", err
	}

	putOptions := []distribution.ManifestServiceOption{distribution.WithTag(tag)}
	digest, err := svc.Put(ctx, manifest, putOptions...)
	if err != nil {
		return "", err
	}
	fmt.Println("  |-> manifest: ", digest.String())

	return digest.String(), err
}
