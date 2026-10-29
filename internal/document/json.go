package document

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"unicode/utf8"

	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

var integer = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// ReadDocument bounds even a stream whose size is not known up front.
func ReadDocument(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxDocumentBytes {
		return nil, failure("ResourceLimit", "document exceeds 4 MiB")
	}
	return b, nil
}

func Decode(b []byte, dst any) error {
	if len(b) > MaxDocumentBytes {
		return failure("ResourceLimit", "document exceeds 4 MiB")
	}
	if !utf8.Valid(b) || !json.Valid(b) {
		return failure("InvalidInput", "expected one valid UTF-8 JSON document")
	}
	// encoding/json replaces unpaired UTF-16 escapes. Reject them before decoding.
	if err := CheckSurrogates(b); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, err := readValue(d, 0)
	if err != nil {
		return err
	}
	if err := shape(v, reflect.TypeOf(dst).Elem(), "$"); err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return failure("InvalidInput", err.Error())
	}
	return nil
}

func CheckSurrogates(b []byte) error {
	inString := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			inString = !inString
		} else if inString && b[i] == '\\' {
			i++
			if b[i] != 'u' {
				continue
			}
			n, _ := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
			i += 4
			if n >= 0xdc00 && n <= 0xdfff {
				return failure("InvalidInput", "unpaired low surrogate")
			}
			if n >= 0xd800 && n <= 0xdbff {
				if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
					return failure("InvalidInput", "unpaired high surrogate")
				}
				low, _ := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
				if low < 0xdc00 || low > 0xdfff {
					return failure("InvalidInput", "invalid surrogate pair")
				}
				i += 6
			}
		}
	}
	return nil
}

func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, failure("ResourceLimit", "JSON nesting exceeds 32")
	}
	t, err := d.Token()
	if err != nil {
		return nil, failure("InvalidInput", err.Error())
	}
	switch v := t.(type) {
	case json.Delim:
		if v == '{' {
			obj := map[string]any{}
			for d.More() {
				k, _ := d.Token()
				key := k.(string)
				if _, exists := obj[key]; exists {
					return nil, failure("InvalidInput", fmt.Sprintf("duplicate JSON key %q", key))
				}
				value, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				obj[key] = value
			}
			d.Token() // json.Valid already checked matching delimiters.
			return obj, nil
		}
		arr := []any{}
		for d.More() {
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, value)
		}
		d.Token()
		return arr, nil
	case string:
		if len(v) > 2048 {
			return nil, failure("ResourceLimit", "string exceeds 2048 UTF-8 bytes")
		}
	case json.Number:
		n, err := strconv.ParseUint(string(v), 10, 64)
		if !integer.MatchString(string(v)) || err != nil || n > 9007199254740991 {
			return nil, failure("InvalidInput", "numbers must be nonnegative exact integers <= 2^53-1")
		}
	case nil:
		return nil, failure("InvalidInput", "null is unsupported; supply every field explicitly")
	}
	return t, nil
}

// All schema fields are required and case sensitive, including empty arrays.
// This avoids Go's default case-insensitive field matching and missing defaults.
func shape(v any, t reflect.Type, path string) error {
	bad := func() error { return failure("InvalidInput", "wrong type or unknown/missing field at "+path) }
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok || len(obj) != t.NumField() {
			return bad()
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := f.Tag.Get("json")
			child, ok := obj[key]
			if !ok {
				return failure("InvalidInput", "missing field at "+path+"."+key)
			}
			if err := shape(child, f.Type, path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return bad()
		}
		for i, child := range arr {
			if err := shape(child, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			return bad()
		}
	case reflect.Bool:
		if _, ok := v.(bool); !ok {
			return bad()
		}
	case reflect.Int, reflect.Int64:
		if _, ok := v.(json.Number); !ok {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

func Canonical(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(b)
}

func Digest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
