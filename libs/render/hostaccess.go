package render

import (
	"fmt"
	"strconv"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// hostAccess writes the entry's publish as ports and its host_mounts as bind mounts on the
// service of instance i (spec 0013, Host access). A {config} host port or path is the value's
// Compose variable, so you can change it in .env. The managed flavor refuses such an instance
// before rendering. An {install} path, platform flavor only (spec 0015), is ${MP_DIR}/<relative>,
// and a host folder with create: true is one setup.sh makes.
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
		if (h.Host.Install != "" || h.Path.Install != "") && r.flavor != flavorPlatform {
			return fmt.Errorf("%w: {install} in host_mounts of module %s belongs to the platform flavor", ErrNotRendered, m.Name)
		}
		if h.Create {
			r.data.Folders = append(r.data.Folders, h.Host.Install)
		}
		svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{
			Type:     types.VolumeTypeBind,
			Source:   r.hostPath(i, m, h.Host),
			Target:   r.hostPath(i, m, h.Path),
			ReadOnly: h.ReadOnly == nil || *h.ReadOnly,
		})
	}
	return nil
}

// hostPath writes one side of a host mount: an absolute path, a config value's Compose variable,
// or ${MP_DIR}/<relative> for an {install} path.
func (r *renderer) hostPath(i string, m contract.Module, p contract.HostPath) string {
	switch {
	case p.Install != "":
		return "${MP_DIR}/" + escape(p.Install)
	case p.Config != "":
		return r.composeVar(i, p.Config, m.Config[p.Config])
	}
	return escape(p.Path)
}
