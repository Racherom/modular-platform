package contract

import (
	"fmt"
	"path"
	"strings"
)

// hostAccess checks publish and host_mounts (spec 0013, Fields): ports in range, a {config}
// host port naming an int value, host paths absolute, {config} paths naming a text value,
// {install} paths relative and inside the install folder, container paths absolute or {install}
// and never mounted twice, and create only on an {install} host folder.
func (c *moduleChecker) hostAccess() {
	for i, p := range c.m.Publish {
		pp := fmt.Sprintf("publish.%d", i)
		if p.Port < 1 || p.Port > 65535 {
			c.add(pp+".port", CodeInvalidPort, "port %d must be between 1 and 65535", p.Port)
		}
		switch {
		case p.Host.Config != "":
			v, ok := c.m.Config[p.Host.Config]
			if !ok || v.Type != ConfigValueTypeInt {
				c.add(pp+".host", CodeInvalidRule, "host names config value %q, which is not a declared int value", p.Host.Config)
			}
		case p.Host.Number < 1 || p.Host.Number > 65535:
			c.add(pp+".host", CodeInvalidPort, "host port %d must be between 1 and 65535", p.Host.Number)
		}
	}
	for i, h := range c.m.HostMounts {
		hp := fmt.Sprintf("host_mounts.%d", i)
		c.hostPath(hp+".host", h.Host)
		switch {
		case h.Path.Config != "":
			c.add(hp+".path", CodeInvalidMount, "the container path is an absolute path or {install: <path>}, not {config}")
		case h.Path.Install != "":
			if c.installPath(hp+".path", h.Path.Install) {
				c.claim(hp+".path", "{install}/"+h.Path.Install)
			}
		default:
			c.mount(hp+".path", h.Path.Path)
		}
		if h.Create && h.Host.Install == "" {
			c.add(hp+".create", CodeInvalidRule, "create makes an {install} folder; this host path is not one")
		}
	}
}

// hostPath checks the host side of a host mount.
func (c *moduleChecker) hostPath(p string, h HostPathRef) {
	switch {
	case h.Config != "":
		v, ok := c.m.Config[h.Config]
		if !ok || v.Type == ConfigValueTypeSecret {
			c.add(p, CodeInvalidRule, "host names config value %q, which is not a declared text value", h.Config)
		}
	case h.Install != "":
		c.installPath(p, h.Install)
	case !path.IsAbs(h.Path) || path.Clean(h.Path) != h.Path:
		c.add(p, CodeInvalidMount, "%q must be a clean absolute path", h.Path)
	}
}

// installPath checks an {install} path: clean, relative and inside the install folder.
func (c *moduleChecker) installPath(p, rel string) bool {
	if rel == "" || rel == "." || path.IsAbs(rel) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		c.add(p, CodeInvalidMount, "{install: %q} must be a clean relative path inside the install folder", rel)
		return false
	}
	return true
}

// claim takes container path target for the dotted path p, refusing a second mount on it.
func (c *moduleChecker) claim(p, target string) {
	if first := c.mounts[target]; first != "" {
		c.add(p, CodeInvalidMount, "%s is already mounted by %s", target, first)
		return
	}
	c.mounts[target] = p
}

// HostAccess names every reason a deployed graph refuses an instance of m, joined into one
// phrase such as "publishes host ports and adds the Linux capability SYS_ADMIN" (spec 0022):
// publish, host_mounts, and the cap_add names UndeployableLinuxCaps lists. It is "" when a
// deployed graph may run m.
func (m Module) HostAccess() string {
	var reasons []string
	if len(m.Publish) > 0 {
		reasons = append(reasons, "publishes host ports")
	}
	if len(m.HostMounts) > 0 {
		reasons = append(reasons, "mounts host paths")
	}
	switch caps := m.UndeployableLinuxCaps(); len(caps) {
	case 0:
	case 1:
		reasons = append(reasons, "adds the Linux capability "+caps[0])
	default:
		reasons = append(reasons, "adds the Linux capabilities "+joinAnd(caps))
	}
	return joinAnd(reasons)
}

// joinAnd joins items as "A", "A and B" or "A, B and C".
func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
