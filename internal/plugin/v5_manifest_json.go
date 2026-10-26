package plugin

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const PluginV5ManifestMaxBytes = 1 << 20

// v5JSON reads untrusted contract bytes without case folding, duplicate keys,
// replacement Unicode, unbounded nesting, or a second JSON document.
func v5JSON(raw []byte) (any, bool) {
	if len(raw) == 0 || len(raw) > PluginV5ManifestMaxBytes || !utf8.Valid(raw) || !v5Unicode(raw) {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, bool)
	read = func(depth int) (any, bool) {
		nodes++
		if depth > 32 || nodes > 20000 {
			return nil, false
		}
		t, err := d.Token()
		if err != nil || t == nil {
			return nil, false
		}
		switch t {
		case json.Delim('{'):
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				k, ok := key.(string)
				if err != nil || !ok {
					return nil, false
				}
				if _, duplicate := m[k]; duplicate {
					return nil, false
				}
				v, ok := read(depth + 1)
				if !ok {
					return nil, false
				}
				m[k] = v
			}
			end, err := d.Token()
			return m, err == nil && end == json.Delim('}')
		case json.Delim('['):
			a := []any{}
			for d.More() {
				v, ok := read(depth + 1)
				if !ok {
					return nil, false
				}
				a = append(a, v)
			}
			end, err := d.Token()
			return a, err == nil && end == json.Delim(']')
		default:
			switch t.(type) {
			case string, bool, json.Number:
				return t, true
			}
			return nil, false
		}
	}
	v, ok := read(0)
	_, err := d.Token()
	return v, ok && err == io.EOF
}

func v5Unicode(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil || n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		i += 4
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

type v5Check func(any) bool

func v5Object(required string, fields map[string]v5Check) v5Check {
	return func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		for _, name := range strings.Fields(required) {
			if _, exists := m[name]; !exists {
				return false
			}
		}
		for k, value := range m {
			check, exists := fields[k]
			if !exists || !check(value) {
				return false
			}
		}
		return true
	}
}

func v5String(min, max int, pattern string) v5Check {
	r := regexp.MustCompile(pattern)
	return func(v any) bool {
		s, ok := v.(string)
		return ok && len([]rune(s)) >= min && len([]rune(s)) <= max && r.MatchString(s)
	}
}
func v5Enum(values ...string) v5Check {
	return func(v any) bool {
		for _, s := range values {
			if v == s {
				return true
			}
		}
		return false
	}
}
func v5Integer(min, max int64) v5Check {
	return func(v any) bool {
		n, ok := v.(json.Number)
		if !ok || len(n) > 128 {
			return false
		}
		r, ok := exactPluginV5Number(n)
		return ok && r.IsInt() && r.Num().IsInt64() && r.Num().Int64() >= min && r.Num().Int64() <= max
	}
}
func v5Array(min, max int, unique bool, check v5Check) v5Check {
	return func(v any) bool {
		a, ok := v.([]any)
		if !ok || len(a) < min || len(a) > max {
			return false
		}
		seen := map[string]bool{}
		for _, item := range a {
			if !check(item) {
				return false
			}
			if unique {
				wire, _ := json.Marshal(item)
				if seen[string(wire)] {
					return false
				}
				seen[string(wire)] = true
			}
		}
		return true
	}
}

func v5PackagePath(v any) bool {
	s, ok := v.(string)
	return ok && len(s) <= 512 && v5PathPattern.MatchString(s) && s != "." && s != ".." && path.Clean(s) == s && !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "../")
}

var (
	v5Name         = v5String(2, 63, `^[a-z0-9][a-z0-9-]{1,62}$`)
	v5Version      = v5String(1, 128, `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)
	v5HostVersion  = v5String(1, 128, `^campusos\.host/v[1-9][0-9]*$`)
	v5Digest       = v5String(71, 71, `^sha256:[0-9a-f]{64}$`)
	v5Capability   = v5String(3, 128, `^[a-z][a-z0-9_.]{2,127}$`)
	v5ConfigName   = v5String(1, 64, `^[a-z][a-z0-9_.-]{0,63}$`)
	v5CollectionID = v5String(1, 64, `^[a-zA-Z0-9_-]{1,64}$`)
	v5Point        = v5Enum("preview.renderer", "ai.chat", "knowledge.source")
	v5Mode         = v5Enum("authorized_resource", "public_only", "no_host_private_data")
	v5PathPattern  = regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`)
)

// Normalize only exactly represented safe integers before decoding typed DTOs;
// JSON's mathematical integers such as 1e3 remain interoperable without float64.
func v5CanonicalNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if len(x) <= 128 {
			if n, ok := exactPluginV5Number(x); ok && n.IsInt() && n.Num().BitLen() <= 53 {
				return json.Number(n.Num().String())
			}
		}
	case map[string]any:
		for k, value := range x {
			x[k] = v5CanonicalNumbers(value)
		}
	case []any:
		for i, value := range x {
			x[i] = v5CanonicalNumbers(value)
		}
	}
	return v
}
