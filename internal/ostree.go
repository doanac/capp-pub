package internal

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	compose "github.com/compose-spec/compose-go/types"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/pkg/archive"
	ostree "github.com/ostreedev/ostree-go/pkg/otbuiltin"
)

func extractImage(ctx context.Context, image string, destDir string) error {
	regc := NewRegistryClient()

	fmt.Printf("Extracting %s -> %s\n", image, destDir)
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return err
	}

	repo, err := regc.GetRepository(ctx, named)
	if err != nil {
		return err
	}
	digest := named.(reference.Digested)

	mansvc, err := repo.Manifests(ctx, nil)
	if err != nil {
		return fmt.Errorf("Unable to get manifest service for %s: %s", image, err)
	}
	man, err := mansvc.Get(ctx, digest.Digest())
	if err != nil {
		return fmt.Errorf("Unable to get image manifests(%s): %s", image, err)
	}

	blobStore := repo.Blobs(ctx)
	switch m := man.(type) {
	case *schema2.DeserializedManifest:
		for i, l := range m.Layers {
			fmt.Printf("  | Layer %d of %d: %d bytes\n", i+1, len(m.Layers), l.Size)
			f, err := blobStore.Open(ctx, l.Digest)
			if err != nil {
				return fmt.Errorf("Unable to open blob %s: %s", l.Digest, err)
			}
			df, err := archive.DecompressStream(f)
			if err != nil {
				return err
			}
			err = archive.Unpack(df, destDir, &archive.TarOptions{})
			f.Close()
			if err != nil {
				return err
			}
		}
		fmt.Println("  |-> ")
		break
	default:
		return fmt.Errorf("Unexpected manifest: %v", man)
	}
	return nil
}

func ostreeCommit(path, repoDir, subject, branch string) (string, error) {
	fmt.Println("Commiting to OSTree")
	ostree.Init(repoDir, ostree.NewInitOptions())
	repo, err := ostree.OpenRepo(repoDir)
	if err != nil {
		return "", fmt.Errorf("Unable to open ostree-repo %s: %s", repoDir, err)
	}
	opts := ostree.NewCommitOptions()
	opts.Subject = subject
	_, err = repo.PrepareTransaction()
	if err != nil {
		return "", fmt.Errorf("Unable to prepare ostree transaction: %s", err)
	}
	ret, err := repo.Commit(path, branch, opts)
	if err != nil {
		return "", fmt.Errorf("Unable to commit %s to ostree: %s", path, err)
	} else {
		fmt.Println("  |->", ret)
	}
	_, err = repo.CommitTransaction()
	if err != nil {
		return "", fmt.Errorf("Unable to commit ostree transaction: %s", err)
	}
	return ret, nil
}

func OstreeCommit(ctx context.Context, ostreeRepo string, proj *compose.Project, configs ServiceConfigs) (map[string][]byte, error) {
	hashes := make(map[string][]byte)
	return hashes, proj.WithServices(nil, func(s compose.ServiceConfig) error {
		for _, containerConfig := range configs[s.Name] {
			fname := s.Name + "/"
			if len(containerConfig.Platform) == 0 {
				fname += "default"
			} else {
				fname += containerConfig.Platform
			}
			named, err := reference.ParseNormalizedNamed(s.Image)
			if err != nil {
				return err
			}
			pinned := named.Name() + "@" + containerConfig.Digest.String()

			dir, err := ioutil.TempDir("", "capp-pub")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)

			if err := extractImage(ctx, pinned, dir); err != nil {
				return err
			}

			hash, err := ostreeCommit(dir, ostreeRepo, pinned, s.Name)
			if err != nil {
				return err
			}
			hashes[fname] = []byte(hash)
		}
		return nil
	})
}
