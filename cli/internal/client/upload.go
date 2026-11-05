package client

import (
	"context"
	"fmt"
	"io"
)

// PutStream performs a PUT whose body is body's raw bytes, sent as
// application/octet-stream with Content-Length set to size. It is used for the
// OS image upload content, the only contract route that is not JSON because it
// carries a potentially multi-gigabyte artifact. out receives the decoded JSON
// success body.
//
// The body is streamed as it is read, never buffered whole, so the artifact does
// not have to fit in memory. size must be the exact number of bytes body yields:
// the api-server checks it against the upload, and net/http fails the request if
// body ends sooner. Because a stream cannot be replayed, a 401 is not retried
// after a Session refresh (see Do); the Session is refreshed before the request
// when it expires soon. The Client's per-request timeout should be disabled
// (Options.Timeout <= 0), matching the contract note that the upload is not
// bounded by the short catalog read timeout.
func (c *Client) PutStream(ctx context.Context, path string, body io.Reader, size int64, out any) error {
	err := c.JSON(ctx, Request{
		Method:        "PUT",
		Path:          path,
		RawBody:       body,
		ContentType:   "application/octet-stream",
		ContentLength: size,
		Auth:          AuthBearer,
	}, out)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	return nil
}
