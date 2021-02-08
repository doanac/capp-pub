package internal

import (
	"encoding/json"
	"fmt"

	compose "github.com/compose-spec/compose-go/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func command(s compose.ServiceConfig, c container.Config) []string {
	if len(s.Command) > 0 {
		return s.Command
	}
	if len(c.Entrypoint) > 0 {
		if len(c.Cmd) > 0 {
			return append(c.Entrypoint, c.Cmd...)
		}
		return c.Entrypoint
	}
	return c.Cmd
}

func RuncSpec(s compose.ServiceConfig, containerConfigBytes []byte) ([]byte, error) {
	var fullconfig struct {
		Config container.Config `json:"config"`
	}
	if err := json.Unmarshal(containerConfigBytes, &fullconfig); err != nil {
		return nil, err
	}
	containerConfig := fullconfig.Config

	spec := oci.DefaultSpec()
	spec.Process.Args = strslice.StrSlice(command(s, containerConfig))
	spec.Process.Cwd = "/" // TODO

	for _, v := range s.Volumes {
		fmt.Println(v.Bind, v.Source, v.Target, v.ReadOnly, v.Type)
		spec.Mounts = append(spec.Mounts, specs.Mount{
			Destination: v.Target,
			Type:        v.Type,
			Source:      v.Source,
			Options:     []string{v.Type, "rprivate", "ro"},
		})
	}
	return json.MarshalIndent(spec, "", "  ")
}

func CreateSpecs(proj *compose.Project, configs ServiceConfigs) (map[string][]byte, error) {
	specs := make(map[string][]byte)
	return specs, proj.WithServices(nil, func(s compose.ServiceConfig) error {
		for _, containerConfig := range configs[s.Name] {
			fname := s.Name + "/"
			if len(containerConfig.Platform) == 0 {
				fname += "default"
			} else {
				fname += containerConfig.Platform
			}
			fmt.Println("Creating runc spec for", fname)
			spec, err := RuncSpec(s, containerConfig.Config)
			if err != nil {
				return err
			}
			specs[fname] = spec
		}
		return nil
	})
}
