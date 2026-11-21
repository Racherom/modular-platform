package setup

import (
	"context"
	"net/url"
)

// IssuerIdentity is the Identity port as api's own config gives it (spec 0037, AC-16): the origin
// is the scheme and host of the OIDC issuer, and the path is the oidc field first_admin_path,
// which the bound provider fills from its first_admin block. A provider without the block leaves
// the path empty, so FirstAdmin answers ok false and no identity module is named here.
type IssuerIdentity struct {
	origin string
	path   string
}

var _ Identity = IssuerIdentity{}

// NewIssuerIdentity builds the port from the issuer URL and the optional first admin path.
func NewIssuerIdentity(issuer *url.URL, firstAdminPath *string) IssuerIdentity {
	id := IssuerIdentity{}
	if issuer != nil && firstAdminPath != nil && *firstAdminPath != "" {
		id.origin = issuer.Scheme + "://" + issuer.Host
		id.path = *firstAdminPath
	}
	return id
}

// FirstAdmin tells where the first admin page is; ok is false when the provider has none.
func (i IssuerIdentity) FirstAdmin(context.Context) (FirstAdmin, bool, error) {
	if i.path == "" {
		return FirstAdmin{}, false, nil
	}
	return FirstAdmin{Origin: i.origin, Path: i.path}, true, nil
}
