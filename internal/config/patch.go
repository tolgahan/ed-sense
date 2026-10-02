package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Why a settings patch was refused, in Problem.Code.
const (
	CodeUnknown  = "unknown"  // no such setting (paths are exact case)
	CodeReadonly = "readonly" // set elsewhere: the controller app (backend.choose) or the file's version
	CodeType     = "type"     // the wrong kind of value, a null inside an entry, or nesting the Schema lacks
	CodeRange    = "range"    // a number out of its range
	CodeEnum     = "enum"     // a word that is not one of the choices
	CodeCount    = "count"    // the wrong number of values
	CodeBroken   = "broken"   // edsense.json does not parse, so nothing is written
	CodeMissing  = "missing"  // a folder setting names a folder that is not there
)

// Problem is one reason a patch was refused. Path uses dots, as
// json.UnmarshalTypeError.Field does: "triggers.hit.params.1"; "" is the
// patch itself.
type Problem struct {
	Path string `json:"path"`
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

// MergePatch applies a settings patch to cur, the file's settings, as
// RFC 7396 merges JSON: objects merge, arrays are replaced whole, and null
// puts a whole setting or a whole map entry back to its default. Every
// value the patch sets is checked against the Schema, and a trigger it
// changes is checked whole once merged; any problem refuses the whole
// patch and returns cur. cur's config_version is kept. changed lists the
// settings and map entries ("triggers.hit") whose value changed, in the
// Schema's order. cur is never changed.
func MergePatch(cur Config, patch []byte) (next Config, changed []string, problems []Problem) {
	v, err := decodeJSON(patch)
	obj, ok := v.(map[string]any)
	if err != nil || !ok {
		return cur, nil, []Problem{{Code: CodeType, Msg: "a patch is a JSON object"}}
	}
	var c checker
	c.patch(obj)
	if len(c.problems) > 0 {
		return cur, nil, c.problems
	}
	target, err := configDoc(cur)
	if err != nil {
		return cur, nil, []Problem{{Code: CodeType, Msg: err.Error()}}
	}
	raw, err := json.Marshal(mergeJSON(target, obj))
	if err == nil {
		next = Default() // a deleted key takes its default, as in Load
		err = json.Unmarshal(raw, &next)
	}
	if err != nil {
		return cur, nil, []Problem{{Code: CodeType, Msg: err.Error()}}
	}
	next.normalise()
	before, _ := configDoc(cur)
	after, err := configDoc(next)
	if err != nil {
		return cur, nil, []Problem{{Code: CodeType, Msg: err.Error()}}
	}
	changed = diff(before, after)
	for _, path := range changed {
		name, ok := strings.CutPrefix(path, "triggers.")
		if t, kept := next.TriggerFX[name]; ok && kept {
			c.trigger(path, t)
		}
	}
	if len(c.problems) > 0 {
		return cur, nil, c.problems
	}
	return next, changed, nil
}

// patch walks a patch against the Schema, with exact-case paths.
func (c *checker) patch(obj map[string]any) {
	index := schemaIndex()
	for _, name := range sortedKeys(obj) {
		v := obj[name]
		k, ok := index[name]
		switch {
		case !ok:
			c.add(name, CodeUnknown, "not a setting")
		case !k.Editable:
			c.add(name, CodeReadonly, "not set here")
		case v == nil: // back to the default
		case !isMap(k):
			c.value(k, name, v)
		default:
			entries, ok := v.(map[string]any)
			if !ok {
				c.add(name, CodeType, "should be an object")
				continue
			}
			for _, e := range sortedKeys(entries) {
				path := name + "." + e
				if c.entryName(k, path, e) && entries[e] != nil {
					c.entry(k, path, e, entries[e])
				}
			}
		}
	}
}

// mergeJSON is RFC 7396's MergePatch. It changes target and returns it.
func mergeJSON(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, ok := target.(map[string]any)
	if !ok {
		t = map[string]any{}
	}
	for k, v := range p {
		if v == nil {
			delete(t, k)
		} else {
			t[k] = mergeJSON(t[k], v)
		}
	}
	return t
}

// diff lists the settings, and the entries of map settings, that differ
// between two JSON forms of Config.
func diff(before, after map[string]any) []string {
	var out []string
	for _, k := range rows() {
		name := keyName(k)
		b, a := before[name], after[name]
		if !isMap(k) {
			if !reflect.DeepEqual(b, a) {
				out = append(out, name)
			}
			continue
		}
		bm, _ := b.(map[string]any)
		am, _ := a.(map[string]any)
		seen := map[string]bool{}
		for _, m := range []map[string]any{bm, am} {
			for _, e := range sortedKeys(m) {
				if !seen[e] && !reflect.DeepEqual(bm[e], am[e]) {
					seen[e] = true
				}
			}
		}
		for _, e := range sortedKeys(seen) {
			out = append(out, name+"."+e)
		}
	}
	return out
}
