package render

import (
	"fmt"
	"strconv"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// hostAccess writes the entry's publish as ports and its host_mounts as bind mounts on the
// service of instance i (spec 0013, Host access). A {config} host port or path is the value's
// Compose variable, so you can change it in .env. Only the static flavor gets here: the managed
// flavor refuses such an instance before rendering, and {install} paths wait for the platform
// flavor (0015).
func (r *renderer) hostAccess(svc *types.ServiceConfig, i string, m contract.Module) error {
	for _, p := range m.Publish {
		host := strconv.Itoa(p.Host.Number)
		if p.Host.Config != "" {
			host = r.composeVar(i, p.Host.Config, m.Config[p.Host.Config])
		}
		svc.Ports = append(svc.Ports, types.ServicePortConfig{
			Target:    uint32(p.Port), //nolint:gosec // CheckModule keeps the port between 1 and 65535
			Published: host,
		})
	}
	for _, h := range m.HostMounts {
		if h.Host.Install != "" || h.Path.Install != "" {
			return fmt.Errorf("%w: {install} in host_mounts of module %s belongs to the platform flavor", ErrNotRendered, m.Name)
		}
		source := escape(h.Host.Path)
		if h.Host.Config != "" {
			source = r.composeVar(i, h.Host.Config, m.Config[h.Host.Config])
		}
		svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{
			Type:     types.VolumeTypeBind,
			Source:   source,
			Target:   escape(h.Path.Path),
			ReadOnly: h.ReadOnly == nil || *h.ReadOnly,
		})
	}
	return nil
}
