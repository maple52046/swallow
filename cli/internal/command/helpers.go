package command

import (
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// query is a small builder over url.Values that omits empty values, so a command
// can accumulate optional filters without a conditional per parameter.
type query struct {
	values url.Values
}

func newQuery() *query { return &query{values: url.Values{}} }

// set adds key=value only when value is non-empty, matching the contract's
// "omit the parameter" semantics for optional query filters.
func (q *query) set(key, value string) *query {
	if value != "" {
		q.values.Set(key, value)
	}
	return q
}

// setInt adds key=value only when the flag was explicitly provided, so an unset
// integer flag does not send a zero that the server would treat as a real value.
func (q *query) setInt(cmd *cobra.Command, key, flag string) *query {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetInt(flag)
		q.values.Set(key, strconv.Itoa(v))
	}
	return q
}

// setBool adds key=true/false only when the flag was explicitly provided.
func (q *query) setBool(cmd *cobra.Command, key, flag string) *query {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetBool(flag)
		q.values.Set(key, strconv.FormatBool(v))
	}
	return q
}

func (q *query) build() url.Values { return q.values }

// addPagination registers the shared --page/--page-size flags for list commands
// that use the contract pagination envelope.
func addPagination(cmd *cobra.Command) {
	cmd.Flags().Int("page", 1, "page number")
	cmd.Flags().Int("page-size", 20, "items per page (max 100)")
}

// applyPagination copies explicitly-set pagination flags into a query builder.
func applyPagination(cmd *cobra.Command, q *query) {
	q.setInt(cmd, "page", "page").setInt(cmd, "pageSize", "page-size")
}

// getJSON issues a GET and prints the decoded payload with the resolved output
// format. It is the common body of every read command.
func getJSON(cmd *cobra.Command, path string, q url.Values) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	var out any
	if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: path, Query: q, Auth: client.AuthBearer}, &out); err != nil {
		return err
	}
	return printResult(out)
}

// sendJSON issues a request with an optional JSON body and prints the decoded
// response. It is the common body of create/update/action commands that return
// a JSON payload.
func sendJSON(cmd *cobra.Command, method, path string, q url.Values, body any) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	var out any
	if err := c.JSON(ctx(cmd), client.Request{Method: method, Path: path, Query: q, Body: body, Auth: client.AuthBearer}, &out); err != nil {
		return err
	}
	if out == nil {
		// A 204/empty success still deserves operator feedback.
		return printResult(map[string]any{"success": true})
	}
	return printResult(out)
}

// sendNoContent issues a request whose success carries no useful body (for
// example a 204 delete) and reports a uniform success object.
func sendNoContent(cmd *cobra.Command, method, path string, q url.Values, body any) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	if err := c.Discard(ctx(cmd), client.Request{Method: method, Path: path, Query: q, Body: body, Auth: client.AuthBearer}); err != nil {
		return err
	}
	return printResult(map[string]any{"success": true})
}

// newUploadClient builds a client with the request timeout disabled, for the
// streamed image upload whose artifact can be too large for the default
// per-request deadline.
func newUploadClient() (*client.Client, error) {
	return client.New(clientOptions(0))
}

// nameFromPath returns the base filename for a multipart upload part.
func nameFromPath(path string) string {
	return filepath.Base(path)
}
