package jargon

// Dictionary is a structure for containing tags and synonyms, for easy passing around
type Dictionary interface {
	GetTags() []string
	GetSynonyms() map[string]string
}
