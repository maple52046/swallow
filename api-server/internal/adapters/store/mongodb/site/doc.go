// Package site is the MongoDB store of Sites (`sites`) and their provider Integrations
// (`integrations`). An Integration's credential is sealed with shared/secret before it is written
// and opened only by the explicit credential read; ordinary reads never decode it.
//
// It maps documents to and from the site domain; connection testing and provider behavior belong
// to the provider adapters.
package site
