package internal

import (
	"encoding/json"
	"fmt"
	"strings"

	compose "github.com/compose-spec/compose-go/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/oci"
	"github.com/docker/docker/oci/caps"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/docker/profiles/seccomp"
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
		//TODO systemd should handle this now
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
	}

	return nil
}

// Based on WithSysctls from
//  https://raw.githubusercontent.com/moby/moby/6458f750e18ad808331e3e6a81c56cc9abe87b91/daemon/oci_linux.go
func setSysctls(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) {
	for k, v := range svc.Sysctls {
		spec.Linux.Sysctl[k] = v
	}
}

// Based on WithOOMScore from
//  https://raw.githubusercontent.com/moby/moby/6458f750e18ad808331e3e6a81c56cc9abe87b91/daemon/oci_linux.go
func setOOMScore(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) {
	score := int(svc.OomScoreAdj)
	spec.Process.OOMScoreAdj = &score
}

// Based on WithCapabilities from
//  https://raw.githubusercontent.com/moby/moby/6458f750e18ad808331e3e6a81c56cc9abe87b91/daemon/oci_linux.go
func setCapabilities(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) error {
	// TODO - a privileged containter in docker produces:
	// /proc/status | CapEff 000003ffffffffff
	// This code does:
	// /proc/status | CapEff 000001ffffffffff
	// It seems it might be due to a newer version of oci library
	capabilities, err := caps.TweakCapabilities(
		oci.DefaultCapabilities(),
		svc.CapAdd,
		svc.CapDrop,
		nil,
		svc.Privileged,
	)
	if err != nil {
		return err
	}
	return oci.SetCapabilities(spec, capabilities)
}

func setLabels(spec *specs.Spec, svc compose.ServiceConfig, c container.Config) {
	spec.Annotations = c.Labels
	if spec.Annotations == nil {
		spec.Annotations = svc.Labels
	} else {
		for k, v := range svc.Labels {
			spec.Annotations[k] = v
		}
	}
}

func setMounts(spec *specs.Spec, svc compose.ServiceConfig) {
	// TODO lots of WithMounts is missing here like ipc mode shm-size
	// and docker-compose volumes
	// also missing volumes-from
	for _, v := range svc.Volumes {
		mode := "rw"
		if v.ReadOnly {
			mode = "ro"
		}
		options := []string{"rbind", "rprivate", mode}
		if v.Bind != nil {
			options[0] = v.Bind.Propagation
		}
		source := v.Source
		if v.Type == "tmpfs" {
			source = "tmpfs"
			options = []string{"noexec", "nosuid", "nodev", "rprivate"}
		}
		spec.Mounts = append(spec.Mounts, specs.Mount{
			Destination: v.Target,
			Type:        v.Type,
			Source:      source,
			Options:     options,
		})
	}

	for _, tfs := range svc.Tmpfs {
		spec.Mounts = append(spec.Mounts, specs.Mount{
			Destination: tfs,
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "noexec", "nodev"},
		})
	}
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

	setLabels(&spec, s, containerConfig)
	if err := setCommonOptions(&spec, s, containerConfig); err != nil {
		return nil, err
	}
	setSysctls(&spec, s, containerConfig)
	if err := setCapabilities(&spec, s, containerConfig); err != nil {
		return nil, err
	}
	setMounts(&spec, s)
	setOOMScore(&spec, s, containerConfig)
	/* TODO port these oci_linux.go functions where applicable:
	opts = append(opts,
		WithCgroups(daemon, c),
		WithResources(c),
		WithDevices(daemon, c),
		WithRlimits(daemon, c),
		WithNamespaces(daemon, c),
		WithLibnetwork(daemon, c),
		WithApparmor(c),
		WithSelinux(c),
	)
	if c.NoNewPrivileges {
		opts = append(opts, coci.WithNoNewPrivileges)
	}
	*/

	return json.MarshalIndent(spec, "", "  ")
}

func isSupported(proj *compose.Project) error {
	return proj.WithServices(nil, func(s compose.ServiceConfig) error {
		if len(s.BlkioConfig) > 0 {
			return fmt.Errorf("Unsupported blkio_config: %s", s.BlkioConfig)
		}
		if s.CPUCount > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_count': %d", s.CPUCount)
		}
		if s.CPUPercent > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_percent': %f", s.CPUPercent)
		}
		if s.CPUShares > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_shares': %d", s.CPUShares)
		}
		if s.CPUPeriod > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_period': %d", s.CPUPeriod)
		}
		if s.CPUQuota > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_quota': %d", s.CPUQuota)
		}
		if s.CPURTRuntime > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_rt_runtime': %d", s.CPURTRuntime)
		}
		if s.CPURTPeriod > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_rt_period': %d", s.CPURTPeriod)
		}
		if s.CPUS > 0 {
			return fmt.Errorf("Unsupported/deprecated attribute 'cpus': %f", s.CPUS)
		}
		if len(s.CPUSet) > 0 {
			return fmt.Errorf("Unsupported attribute 'cpu_set': %s", s.CPUSet)
		}
		if s.Build != nil {
			return fmt.Errorf("Unsupported attribute 'build'")
		}
		if len(s.CgroupParent) > 0 {
			return fmt.Errorf("Unsupported attribute 'cgroup_parent': %s", s.CgroupParent)
		}
		if s.Configs != nil {
			return fmt.Errorf("Unsupported attribute 'configs'")
		}
		if len(s.ContainerName) > 0 {
			return fmt.Errorf("Unsupported attribute 'container_name': %s", s.ContainerName)
		}
		if s.CredentialSpec != nil {
			return fmt.Errorf("Unsupported attribute 'credential_spec'")
		}
		if s.DependsOn != nil {
			return fmt.Errorf("Unsupported attribute 'depends_on'")
		}
		if s.Deploy != nil {
			return fmt.Errorf("Unsupported swarm attribute 'deploy'")
		}
		if s.Devices != nil {
			return fmt.Errorf("Unsupported attribute 'devices'")
		}
		if s.DNS != nil {
			return fmt.Errorf("Unsupported attribute 'dns'")
		}
		if s.DNSOpts != nil {
			return fmt.Errorf("Unsupported attribute 'dns_opts'")
		}
		if s.DNSSearch != nil {
			return fmt.Errorf("Unsupported attribute 'dns_search'")
		}
		if s.EnvFile != nil {
			return fmt.Errorf("Unsupported attribute 'env_file'")
		}
		if s.Expose != nil {
			return fmt.Errorf("Unsupported attribute 'expose' (not required)")
		}
		if s.Extends != nil {
			return fmt.Errorf("Unsupported attribute 'extends'")
		}
		if s.ExternalLinks != nil {
			return fmt.Errorf("Unsupported attribute 'external_links'")
		}
		if s.GroupAdd != nil {
			return fmt.Errorf("Unsupported attribute 'group_add'")
		}
		if s.HealthCheck != nil {
			return fmt.Errorf("Unsupported attribute 'healthcheck'")
		}
		if s.Init != nil && *s.Init {
			return fmt.Errorf("Unsupported attribute 'init'")
		}
		if len(s.Ipc) > 0 {
			return fmt.Errorf("Unsupported attribute 'ipc': %s", s.Ipc)
		}
		if len(s.Isolation) > 0 {
			return fmt.Errorf("Unsupported attribute 'isolation': %s", s.Isolation)
		}
		if s.Links != nil {
			return fmt.Errorf("Unsupported attribute 'links'")
		}
		if s.Logging != nil {
			return fmt.Errorf("Unsupported attribute 'logging'")
		}
		if len(s.MacAddress) > 0 {
			return fmt.Errorf("Unsupported attribute 'mac_address': %s", s.MacAddress)
		}
		if s.MemLimit > 0 {
			return fmt.Errorf("Unsupported/deprecated attribute 'mem_limit': %d", s.MemLimit)
		}
		if s.MemReservation > 0 {
			return fmt.Errorf("Unsupported/deprecated attribute 'mem_reservation': %d", s.MemReservation)
		}
		if s.MemSwappiness > 0 {
			return fmt.Errorf("Unsupported attribute 'mem_swapiness': %d", s.MemSwappiness)
		}
		if s.MemSwapLimit > 0 {
			return fmt.Errorf("Unsupported attribute 'memswap_limit': %d", s.MemSwapLimit)
		}
		if len(s.Pid) > 0 {
			return fmt.Errorf("Unsupported attribute 'pid': %s", s.Isolation)
		}
		if s.PidLimit > 0 {
			return fmt.Errorf("Unsupported attribute 'pids_limit': %d", s.PidLimit)
		}
		if len(s.Platform) > 0 {
			return fmt.Errorf("Unsupported attribute 'platform': %s", s.Platform)
		}
		if len(s.Runtime) > 0 {
			return fmt.Errorf("Unsupported attribute 'runtime': %s", s.Runtime)
		}
		if s.Scale > 0 {
			return fmt.Errorf("Unsupported swarm attribute 'scale': %d", s.Scale)
		}
		if s.Secrets != nil {
			return fmt.Errorf("Unsupported attribute 'secrets'")
		}
		if len(s.ShmSize) > 0 {
			return fmt.Errorf("Unsupported attribute 'shm_size': %s", s.ShmSize)
		}
		if s.StdinOpen {
			return fmt.Errorf("Unsupported attribute 'stdin_open: true'")
		}
		if s.StopGracePeriod != nil {
			return fmt.Errorf("Unsupported attribute 'stop_grace_period'")
		}
		if len(s.StopSignal) > 0 {
			return fmt.Errorf("Unsupported attribute 'stop_signal': %s", s.StopSignal)
		}
		if s.Ulimits != nil {
			return fmt.Errorf("Unsupported attribute 'ulimits'")
		}
		if len(s.UserNSMode) > 0 {
			return fmt.Errorf("Unsupported attribute 'userns_mode': %s", s.UserNSMode)
		}
		if s.VolumesFrom != nil {
			return fmt.Errorf("Unsupported attribute 'volumes_from'")
		}
		return nil
	})
}

func CreateSpecs(proj *compose.Project, configs ServiceConfigs) (map[string][]byte, error) {
	if err := isSupported(proj); err != nil {
		return nil, err
	}
	specs := make(map[string][]byte)
	bytes, err := json.MarshalIndent(seccomp.DefaultProfile(), "", "  ")
	if err != nil {
		return nil, err
	}
	specs[".default-secomp.json"] = bytes
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
