// Package server is the MongoDB store of the stable Server projection (`servers`), including each
// Server's Platform claim.
//
// A claim write that meets another Platform's claim refuses without undoing the claims it already
// wrote; the caller's store transaction rolls them back, so callers must claim inside one.
// Publishing Server events after a write is the eventing decorator's job, not this package's.
package server
