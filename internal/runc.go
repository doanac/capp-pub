package internal

import (
	"encoding/json"
	"strings"

	compose "github.com/compose-spec/compose-go/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/oci"
	"github.com/docker/docker/pkg/system"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
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

func env(s compose.ServiceConfig, c container.Config) []string {
	envMap := make(map[string]*string)

	// first populate map from execConfig
	for _, val := range c.Env {
		parts := strings.SplitN(val, "=", 2)
		if len(parts) == 1 {
			envMap[parts[0]] = nil
		} else {
			envMap[parts[0]] = &parts[1]
		}
	}

	// now override with service vals
	for k, v := range s.Environment {
		envMap[k] = v
	}

	// pull in special vals:
	if s.Tty {
		if _, ok := envMap["TERM"]; !ok {
			term := "xterm"
			envMap["TERM"] = &term
		}
	}
	if _, ok := envMap["PATH"]; !ok {
		path := system.DefaultPathEnv("linux")
		envMap["PATH"] = &path
	}
	if _, ok := envMap["HOSTNAME"]; !ok {
		envMap["HOSTNAME"] = &s.Hostname
	}

	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		if v != nil {
			env = append(env, k+"="+*v)
		} else {
			env = append(env, k)
		}
	}
	return env
}

// Based on WithCommonOptions from
//  https://raw.githubusercontent.com/moby/moby/6458f750e18ad808331e3e6a81c56cc9abe87b91/daemon/oci_linux.go
func setCommonOptions(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) error {
	spec.Root.Readonly = svc.ReadOnly

	// TODO WithCommonOptions does setupLinkedContainers. The way capp-run does
	// networking probably means we don't need to do this
	// TODO WithCommonOptions does some permission hacking with c.SetupWorkingDirectory
	spec.Process.Cwd = "/"
	if len(svc.WorkingDir) > 0 {
		spec.Process.Cwd = svc.WorkingDir
	} else if len(c.WorkingDir) > 0 {
		spec.Process.Cwd = c.WorkingDir
	}

	if len(svc.Command) > 0 {
		spec.Process.Args = svc.Command
	} else if len(c.Entrypoint) > 0 {
		if len(c.Cmd) > 0 {
			spec.Process.Args = append(c.Entrypoint, c.Cmd...)
		} else {
			spec.Process.Args = c.Entrypoint
		}
	} else {
		spec.Process.Args = c.Cmd
	}

	spec.Process.Env = env(svc, c)

	if svc.Tty {
		spec.Process.Terminal = true
		return errors.New("TODO - capp-run does not support tty:True")
	}

	if len(svc.Hostname) > 0 {
		spec.Hostname = svc.Hostname
	}

	spec.Linux.Sysctl = make(map[string]string)
	if svc.DomainName != "" {
		spec.Linux.Sysctl["kernel.domainname"] = svc.DomainName
	}

	if svc.NetworkMode != "host" {
		// allow unprivileged ICMP echo sockets without CAP_NET_RAW
		spec.Linux.Sysctl["net.ipv4.ping_group_range"] = "0 2147483647"
		// allow opening any port less than 1024 without CAP_NET_BIND_SERVICE
		spec.Linux.Sysctl["net.ipv4.ip_unprivileged_port_start"] = "0"

		return errors.New("TODO - capp-run does not support private networks")
	}

	return nil
}

func setSysctls(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) {
	for k, v := range svc.Sysctls {
		spec.Linux.Sysctl[k] = v
	}
}

func setOOMScore(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) {
	score := int(svc.OomScoreAdj)
	spec.Process.OOMScoreAdj = &score
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

	if err := setCommonOptions(&spec, s, containerConfig); err != nil {
		return nil, err
	}
	setSysctls(&spec, s, containerConfig)
	setOOMScore(&spec, s, containerConfig)
	/* TODO port these oci_linux.go functions where applicable:
	opts = append(opts,
		WithCgroups(daemon, c),
		WithResources(c),
		WithDevices(daemon, c),
		WithRlimits(daemon, c),
		WithNamespaces(daemon, c),
		WithCapabilities(c),
		WithSeccomp(daemon, c),
		WithMounts(daemon, c),
		WithLibnetwork(daemon, c),
		WithApparmor(c),
		WithSelinux(c),
	)
	if c.NoNewPrivileges {
		opts = append(opts, coci.WithNoNewPrivileges)
	}
	*/

	for _, v := range s.Volumes {
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
			spec, err := RuncSpec(s, containerConfig.Config)
			if err != nil {
				return err
			}
			specs[fname] = spec
		}
		return nil
	})
}
