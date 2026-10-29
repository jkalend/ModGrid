package resolver

import (
	"io"
	"modgrid.local/modgrid/internal/document"
)

func ReadDocument(r io.Reader) ([]byte, error) { return document.ReadDocument(r) }
func decode(b []byte, dst any) error           { return document.Decode(b, dst) }
func canonical(v any) ([]byte, error)          { return document.Canonical(v) }
func digest(b []byte) string                   { return document.Digest(b) }
func checkSurrogates(b []byte) error           { return document.CheckSurrogates(b) }
