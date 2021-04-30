package internal

import (
	"fmt"

	compose "github.com/compose-spec/compose-go/types"
)

func systemdRestart(composeRestart string) string {
	switch composeRestart {
	case "no":
		return "no"
	case "always":
		return "always"
	case "on-failure":
		return "on-failure"
	case "unless-stopped":
		return "always"
	case "":
		return "no"
	default:
		panic("Unknown restart value: " + composeRestart)
	}
}

func CreateServices(proj *compose.Project) (map[string][]byte, error) {
	services := make(map[string][]byte)
	services["{{app}}.service"] = []byte(`[Unit]
Description=Compose app

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`)

	svcFmt := `[Unit]
Description=Compose app service
PartOf=capp_{{app}}.service
After=capp_{{app}}.service

[Service]
ExecStart={{binary}} -n {{app}} -d {{appdir}} up %s
SyslogIdentifier={{app}}_%s
Restart=%s

[Install]
WantedBy=capp_{{app}}.service
`
	return services, proj.WithServices(nil, func(s compose.ServiceConfig) error {
		fname := fmt.Sprintf("{{app}}_%s.service", s.Name)
		services[fname] = []byte(fmt.Sprintf(svcFmt, s.Name, s.Name, systemdRestart(s.Restart)))
		return nil
	})
}
