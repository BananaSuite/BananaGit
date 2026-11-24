// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"bytes"
	"encoding/json" //nolint:depguard // the vectors need json.Number, as the cleaner does
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eggVector struct {
	Name         string
	Input        any
	Clean        any
	StrictErrors [][]any `json:"strict_errors"`
}

// TestEggVectors runs every "clean" case of the suite's eggs-vectors.json (a byte-identical copy of the
// shared file); the "legacy" cases are BananaWiki 1.4 imports and do not apply to BananaGit.
func TestEggVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/bananagit-eggs-vectors.json")
	require.NoError(t, err)
	var file struct {
		Spec  string
		Clean []json.RawMessage
	}
	require.NoError(t, json.Unmarshal(data, &file))
	assert.Equal(t, "banana-eggs/v1", file.Spec)
	require.NotEmpty(t, file.Clean)

	for _, raw := range file.Clean {
		var v eggVector
		require.NoError(t, json.Unmarshal(raw, &v)) // expected output with plain float64 numbers
		var withNumbers struct{ Input any }
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		require.NoError(t, dec.Decode(&withNumbers))

		t.Run(v.Name, func(t *testing.T) {
			var cfg *EggConfig
			var errs []EggError
			if text, ok := withNumbers.Input.(string); ok {
				cfg, errs = CleanEggs(text)
			} else {
				cfg, errs = CleanEggsValue(withNumbers.Input)
			}

			var got any
			require.NoError(t, json.Unmarshal([]byte(cfg.Dumps()), &got))
			assert.Equal(t, v.Clean, got)

			gotErrs := [][]any{}
			for _, e := range errs {
				var index any
				if e.Index >= 0 {
					index = float64(e.Index)
				}
				gotErrs = append(gotErrs, []any{index, e.Field, e.Code})
			}
			want := v.StrictErrors
			if want == nil {
				want = [][]any{}
			}
			assert.Equal(t, want, gotErrs)
		})
	}
}

func TestEggCleanEdgeCases(t *testing.T) {
	cfg, errs := CleanEggs(`{"eggs":[{"id":"f","keys":["a","b"],"badge":12.0}]}`)
	assert.Equal(t, []EggError{{0, "badge", "invalid"}}, errs)
	assert.Nil(t, cfg.Eggs[0].Badge)

	cfg, errs = CleanEggs(`{"version":true,"eggs":[{"id":"m","keys":["a","b"],"message":"a\nb\u0007"}]}`)
	assert.Equal(t, []EggError{{0, "message", "invalid"}}, errs) // version true == 1, as in Python
	assert.Equal(t, "a b\u0007", cfg.Eggs[0].Message)

	_, errs = CleanEggs(`{"eggs":[]} {}`)
	assert.Equal(t, []EggError{{-1, "list", "json"}}, errs)

	cfg, errs = CleanEggs(`[{"id":"r","keys":["a","b"],"count":30.5,"speed_min":1.005,"unlock":"x","enabled":null}]`)
	assert.Equal(t, []EggError{{0, "unlock", "type"}, {0, "enabled", "type"}}, errs)
	assert.Equal(t, 30, cfg.Eggs[0].Count)          // round half to even
	assert.InDelta(t, 1.0, cfg.Eggs[0].SpeedMin, 0) // 1.005 is 1.00499... in binary
	assert.Equal(t, "page", cfg.Eggs[0].Unlock.Action)
	assert.True(t, cfg.Eggs[0].Enabled)

	_, errs = CleanEggs(`{"eggs":[{"id":"s","keys":["a","b"],"message":"` + strings.Repeat("x", EggMaxBytes) + `"}]}`)
	assert.Equal(t, []EggError{{-1, "list", "size"}, {0, "message", "invalid"}}, errs)
}

func TestEggDefaultAndClient(t *testing.T) {
	def := DefaultEggConfig()
	again, errs := CleanEggs(def.Dumps())
	assert.Empty(t, errs)
	assert.Equal(t, def, again)
	assert.Equal(t, EggSprite{Type: "emoji", Value: "🍌"}, def.Eggs[0].Sprite)

	tpl := TemplateEgg()
	tpl.ID, tpl.Keys = "t", []string{"a", "b"}
	cleaned, errs := CleanEggs(`[{"id":"t","keys":["a","b"]}]`)
	assert.Empty(t, errs)
	assert.Equal(t, tpl, cleaned.Eggs[0], "the template egg holds the format's defaults")

	cfg, errs := CleanEggs(`[{"id":"on","keys":["a","b"],"badge":"gold","message":"</script><b>&"},` +
		`{"id":"off","enabled":false,"keys":["a","b"]}]`)
	require.Empty(t, errs)
	client := cfg.ClientConfig("/-/bananagit/eggs/{id}/found", "/-/bananagit/eggs", true, "", "", nil)
	require.Len(t, client.Eggs, 1)
	assert.Nil(t, client.CSRF)

	out := EggScriptJSON(client)
	assert.NotContains(t, out, "<")
	assert.NotContains(t, out, ">")
	assert.Contains(t, out, string(rune(92))+"u003c/script")
	assert.NotContains(t, out, "badge")
	assert.NotContains(t, out, `"off"`)
	var back map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &back))
	assert.Equal(t, "</script><b>&", back["eggs"].([]any)[0].(map[string]any)["message"])
	assert.Nil(t, back["csrf"])
	assert.Equal(t, map[string]any{}, back["strings"])
}
