package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormProp describes one property of a Synapse form.
type FormProp struct {
	Type       string `json:"type,omitempty"`
	Doc        string `json:"doc,omitempty"`
	ReadOnly   bool   `json:"ro,omitempty"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

// FormInfo describes a Synapse data model form.
type FormInfo struct {
	Doc   string              `json:"doc"`
	Ex    string              `json:"ex,omitempty"`
	Props map[string]FormProp `json:"props"`
}

// FormsIndex provides fast lookup of Synapse form definitions.
type FormsIndex struct {
	forms map[string]FormInfo
}

// LoadFormsIndex parses forms.json bytes into a lookup index.
func LoadFormsIndex(data []byte) (*FormsIndex, error) {
	if len(data) == 0 {
		return &FormsIndex{forms: map[string]FormInfo{}}, nil
	}
	var m map[string]FormInfo
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse forms.json: %w", err)
	}
	return &FormsIndex{forms: m}, nil
}

func (fi *FormsIndex) Len() int { return len(fi.forms) }

// Get returns the FormInfo for an exact form name, or nil if not found.
func (fi *FormsIndex) Get(name string) *FormInfo {
	if f, ok := fi.forms[name]; ok {
		return &f
	}
	return nil
}

// Search returns up to topK forms whose name contains the query substring
// (case-insensitive), followed by forms whose doc contains it.
func (fi *FormsIndex) Search(query string, topK int) []map[string]interface{} {
	q := strings.ToLower(query)
	var nameHits, docHits []map[string]interface{}

	for name, f := range fi.forms {
		lname := strings.ToLower(name)
		ldoc := strings.ToLower(f.Doc)
		entry := map[string]interface{}{
			"name": name,
			"doc":  f.Doc,
		}
		if f.Ex != "" {
			entry["ex"] = f.Ex
		}
		if len(f.Props) > 0 {
			propNames := make([]string, 0, len(f.Props))
			for p := range f.Props {
				propNames = append(propNames, p)
			}
			entry["props"] = propNames
		}
		if strings.Contains(lname, q) {
			nameHits = append(nameHits, entry)
		} else if strings.Contains(ldoc, q) {
			docHits = append(docHits, entry)
		}
	}

	results := append(nameHits, docHits...)
	if len(results) > topK {
		results = results[:topK]
	}
	return results
}
