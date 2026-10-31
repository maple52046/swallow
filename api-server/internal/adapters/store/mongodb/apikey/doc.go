// Package apikey is the MongoDB store of the API Key context (`api_keys`). Only secret hashes and
// display prefixes are stored; a secret never reaches the database.
//
// It maps documents to and from the apikey domain and owns the collection's indexes. Key issuance,
// validation, and ownership rules belong to the core apikey context, not here.
package apikey
