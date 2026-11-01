// Package s3 is the client of an s3 binding (spec 0005, "Bindings, clients and protection"): a
// MinIO client with path style addressing at the binding's url, signing with its access key and
// secret key file, that trusts the binding's ca_file for an https url.
package s3

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/racherom/modular-platform/sdk"
)

// defaultRegion is the region a binding without region signs for.
const defaultRegion = "us-east-1"

// Client is a MinIO client for one binding, with the binding's bucket.
type Client struct {
	*minio.Client
	Bucket string
}

// Open is the sdk.Opener of an s3 binding. It does not reach the provider; readiness checks the
// bucket exists until it does.
func Open(b sdk.Binding) (sdk.Opened, error) {
	region := b.Region
	if region == "" {
		region = defaultRegion
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if b.Roots != nil {
		tr.TLSClientConfig = &tls.Config{RootCAs: b.Roots, MinVersion: tls.VersionTLS12}
	}
	mc, err := minio.New(b.URL.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(b.AccessKeyID, b.SecretKey.Reveal(), ""),
		Secure:       b.URL.Scheme == "https",
		Transport:    tr,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return sdk.Opened{}, fmt.Errorf("s3: %w", err)
	}
	c := &Client{Client: mc, Bucket: b.Bucket}
	return sdk.Opened{Client: c, Ready: c.Ready, Close: tr.CloseIdleConnections}, nil
}

// Ready checks once that the bucket exists. A missing bucket is not ready yet.
func (c *Client) Ready(ctx context.Context) error {
	ok, err := c.BucketExists(ctx, c.Bucket)
	switch {
	case err != nil:
		return fmt.Errorf("s3: bucket %s: %w", c.Bucket, err)
	case !ok:
		return fmt.Errorf("s3: bucket %s does not exist", c.Bucket)
	}
	return nil
}

// Format shows the endpoint and bucket only, never the credentials.
func (c *Client) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "s3.Client{%s/%s}", c.EndpointURL().Redacted(), c.Bucket)
}

// LogValue shows the endpoint and bucket only, never the credentials.
func (c *Client) LogValue() slog.Value {
	return slog.StringValue(c.EndpointURL().Redacted() + "/" + c.Bucket)
}
