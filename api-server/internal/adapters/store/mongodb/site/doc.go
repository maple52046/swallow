// Package site is the MongoDB store of Sites (`sites`) and their provider Integrations
// (`integrations`). An Integration's credential, and its optional operator credential, are sealed
// with shared/secret before they are written and opened only by their explicit credential reads;
// ordinary reads never decode them.
//
// It maps documents to and from the site domain; connection testing and provider behavior belong
// to the provider adapters.
package site
