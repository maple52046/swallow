package client

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
)

// UploadFile describes the streamed file part of a multipart upload. Field is
// the form field name, Name is the reported filename, and Reader supplies the
// bytes. The reader is streamed, never buffered whole, so a multi-gigabyte image
// artifact does not have to fit in memory.
type UploadFile struct {
	Field  string
	Name   string
	Reader io.Reader
}

// Upload performs a multipart/form-data POST, used by the OS image upload
// endpoint — the only contract route that is not JSON because it carries a large
// image artifact. fields are the simple text parts (for example integrationId,
// name, architecture); file is the streamed artifact. out receives the decoded
// JSON success body.
//
// The multipart body is produced through an io.Pipe so the file streams to the
// server as it is read from disk. The Client's per-request timeout should be
// disabled (Options.Timeout <= 0) for this call, matching the contract note that
// upload is not bounded by the short catalog read timeout.
func (c *Client) Upload(ctx context.Context, path string, fields map[string]string, file UploadFile, out any) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// The writer goroutine owns closing the pipe writer. Any encoding error is
	// propagated to the reader (and thus the HTTP request) via CloseWithError so
	// the request fails instead of hanging on a truncated body.
	go func() {
		var werr error
		defer func() {
			if werr != nil {
				_ = pw.CloseWithError(werr)
				return
			}
			if cerr := mw.Close(); cerr != nil {
				_ = pw.CloseWithError(cerr)
				return
			}
			_ = pw.Close()
		}()

		for k, v := range fields {
			if werr = mw.WriteField(k, v); werr != nil {
				return
			}
		}
		part, err := mw.CreateFormFile(file.Field, file.Name)
		if err != nil {
			werr = err
			return
		}
		if _, err := io.Copy(part, file.Reader); err != nil {
			werr = err
		}
	}()

	err := c.JSON(ctx, Request{
		Method:      "POST",
		Path:        path,
		RawBody:     pr,
		ContentType: mw.FormDataContentType(),
		Auth:        AuthBearer,
	}, out)
	if err != nil {
		// Ensure the writer goroutine unblocks if the request setup failed early.
		_ = pr.CloseWithError(err)
		return fmt.Errorf("upload: %w", err)
	}
	return nil
}
