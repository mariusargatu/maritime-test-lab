// Package graphqlops embeds the committed onBOARD GraphQL operations so the
// client sends exactly the text that the contract-graphql gate validates. The
// .graphql files in this directory ARE the consumer contract corpus.
package graphqlops

import _ "embed"

var (
	//go:embed create_voyage.graphql
	CreateVoyage string

	//go:embed update_voyage.graphql
	UpdateVoyage string

	//go:embed get_voyage.graphql
	GetVoyage string
)
