package qbit

import (
	"encoding/json"
	"testing"
)

// FuzzRuleJSON checks any rule definition decodes without panicking, and
// that writing a rule out and reading it back gives the same rule.
func FuzzRuleJSON(f *testing.F) {
	for _, s := range []string{
		`{"enabled":true,"mustContain":"x","affectedFeeds":"http://a","torrentParams":{"stopped":true,"tags":["a",1,null]}}`,
		`{"addPaused":null,"savePath":"/tv","torrentContentLayout":null,"lastMatch":"20 Nov 2017 09:05:11 +0100"}`,
		`{"priority":1.5,"ignoreDays":-3,"previouslyMatchedEpisodes":[1,"1x2"],"torrentParams":null}`,
		`[]`, `null`, `"x"`, `{}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		var def map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &def)
		r := decodeRule(def)
		out, err := json.Marshal(encodeRule(r))
		if err != nil {
			t.Fatalf("encoding %+v: %v", r, err)
		}
		var again map[string]json.RawMessage
		if err := json.Unmarshal(out, &again); err != nil {
			t.Fatal(err)
		}
		out2, err := json.Marshal(encodeRule(decodeRule(again)))
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(out2) {
			t.Fatalf("round trip changed the rule:\n%s\n%s", out, out2)
		}
	})
}
