package render

import (
	"strconv"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/racherom/modular-platform/libs/contract"
)

// templateFiles renders every template of every instance (spec 0013, "Templates") into
// config/<instance>/<file without .tmpl>, and mounts each read only at its path through the
// Compose config <instance>_<file without .tmpl>. It runs after the bindings, whose values the
// template data holds.
func (r *renderer) templateFiles(bindings []contract.Binding) ([]File, error) {
	clients := map[string]map[string][]contract.TemplateClient{} // provider, provide
	for _, b := range bindings {                                 // sorted by consumer then slot
		if r.g.Modules[b.Provider].External != nil {
			continue
		}
		if clients[b.Provider] == nil {
			clients[b.Provider] = map[string][]contract.TemplateClient{}
		}
		values, err := r.clientValues(b)
		if err != nil {
			return nil, err
		}
		clients[b.Provider][b.Provide] = append(clients[b.Provider][b.Provide], contract.TemplateClient{
			Instance: b.Consumer, Slot: b.Slot, Host: r.host(b.Consumer), Values: values,
		})
	}
	boundTo := map[string]map[string]contract.Binding{} // consumer, slot
	for _, b := range bindings {
		if boundTo[b.Consumer] == nil {
			boundTo[b.Consumer] = map[string]contract.Binding{}
		}
		boundTo[b.Consumer][b.Slot] = b
	}

	var files []File
	for _, i := range sortedKeys(r.g.Modules) {
		gm := r.g.Modules[i]
		if gm.External != nil {
			continue
		}
		m := r.modules[gm.Module]
		if len(m.Templates) == 0 {
			continue
		}
		data := r.templateData(i, m, boundTo[i], clients[i])
		svc := r.project.Services[i]
		for _, t := range m.Templates {
			out, err := r.templates.Execute(m.Name, t.File, data)
			if err != nil {
				return nil, err
			}
			file, config := contract.TemplateOutput(i, t)
			if r.project.Configs == nil {
				r.project.Configs = types.Configs{}
			}
			r.project.Configs[config] = types.ConfigObjConfig{File: "./" + file}
			svc.Configs = append(svc.Configs, types.ServiceConfigObjConfig{Source: config, Target: t.Path})
			files = append(files, File{Path: file, Mode: 0o644, Data: out})
		}
		r.project.Services[i] = svc
	}
	return files, nil
}

// templateData is the data instance i of module m renders its templates with: its provides, every
// slot (bound or not) and every binding to each of its provides.
func (r *renderer) templateData(i string, m contract.Module, bound map[string]contract.Binding, clients map[string][]contract.TemplateClient) contract.TemplateData {
	d := contract.TemplateData{
		Graph:    r.g.Name,
		Instance: i,
		Host:     r.host(i),
		Provides: map[string]contract.TemplateProvide{},
		Uses:     map[string]contract.TemplateSlot{},
		Clients:  map[string][]contract.TemplateClient{},
	}
	for name, q := range m.Provides {
		port, _ := r.address(i, q, r.caps[q.Capability])
		d.Provides[name] = contract.TemplateProvide{Port: port}
		d.Clients[name] = append([]contract.TemplateClient{}, clients[name]...)
	}
	for name := range m.Slots() {
		b, ok := bound[name]
		if !ok {
			d.Uses[name] = contract.TemplateSlot{Values: map[string]string{}}
			continue
		}
		d.Uses[name] = r.templateSlot(b)
	}
	return d
}

// templateSlot is the template data of binding b on its consumer: the provider's address and the
// non secret fields known at check time that it gives. An external provider's address is its host, port and
// url fields, when it gives them.
func (r *renderer) templateSlot(b contract.Binding) contract.TemplateSlot {
	values := map[string]string{}
	for f, v := range r.bound[b.Consumer][b.Slot] {
		if v.isText() {
			values[f] = v.text
		}
	}
	s := contract.TemplateSlot{Bound: true, Provider: b.Provider, Provide: b.Provide, Values: values}
	e := r.end(b.Provider, b.Provide)
	if e.external != nil {
		s.Host, s.URL = values["host"], values["url"]
		s.Port, _ = strconv.Atoi(values["port"])
		return s
	}
	s.Host = r.host(b.Provider)
	var scheme string
	s.Port, scheme = r.address(b.Provider, e.provide, r.caps[e.offered])
	if scheme != "" {
		s.URL = scheme + "://" + s.Host + ":" + strconv.Itoa(s.Port)
	}
	return s
}

// clientValues resolves the to_provider text fields of binding b from the consumer's slot sources
// (spec 0013): a literal, or the url, host or port of one of the consumer's own provides.
func (r *renderer) clientValues(b contract.Binding) (map[string]string, error) {
	out := map[string]string{}
	slot := r.modules[r.g.Modules[b.Consumer].Module].Slots()[b.Slot]
	k := r.caps[b.Capability]
	for f, src := range slot.Fields {
		field := k.Fields[f]
		if field.IsFile() || field.Direction != contract.ToProvider {
			continue
		}
		switch {
		case src.Value != "":
			out[f] = src.Value
			continue
		case src.Platform == "":
			continue // the catalog allows only a literal or a platform fact on a text field
		}
		own := r.end(b.Consumer, src.Provide)
		v, set, err := r.source(own, r.caps[own.offered], f, contract.FieldSource{Platform: src.Platform, Path: src.Path})
		if err != nil {
			return nil, err
		}
		if set && v.isText() {
			out[f] = v.text
		}
	}
	return out, nil
}
