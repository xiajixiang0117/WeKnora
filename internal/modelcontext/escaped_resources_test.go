package modelcontext

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func TestEscapedResourcesRestoreAcrossEveryStreamSplit(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	for _, handle := range []string{`res\://0001`, `res\\\://0001`, `res:\/\/0001`, `res\:\/\/0001`} {
		t.Run(handle, func(t *testing.T) {
			r := NewRegistry(true)
			r.EncodeMessages([]chat.Message{{Role: "tool", Content: ref}})
			input := "![GPIO](" + handle + ")"
			want := "![GPIO](" + ref + ")"
			require.Equal(t, want, r.DecodeOutputText(input))
			for split := 0; split <= len(input); split++ {
				d := r.StreamDecoder()
				got := d.Feed(input[:split]) + d.Feed(input[split:]) + d.Flush()
				require.Equal(t, want, got, "split=%d", split)
			}
			d := r.StreamDecoder()
			var got strings.Builder
			for i := range input {
				got.WriteString(d.Feed(input[i : i+1]))
			}
			got.WriteString(d.Flush())
			require.Equal(t, want, got.String())
		})
	}
}

func TestEscapedOrphanResourcesNeverLeak(t *testing.T) {
	for _, token := range []string{`res\://9999`, `res\\\:\/\/9999`, `res\:`, `res\:/`, `res\://`} {
		input := "missing " + token
		for split := 0; split <= len(input); split++ {
			d := NewRegistry(true).StreamDecoder()
			got := d.Feed(input[:split]) + d.Feed(input[split:]) + d.Flush()
			require.Equal(t, "missing ", got, "input=%q split=%d", input, split)
		}
	}
	r := NewRegistry(true)
	require.Equal(t, "missing ", r.DecodeOutputText(`missing res\://9999`))
	require.Equal(t, []string{"res://9999"}, r.OrphanResourceHandles(`res\://9999`))
	ordinary := "`C:\\Users\\test` and \\(x+y\\) and \\*literal\\*"
	require.Equal(t, ordinary, r.DecodeOutputText(ordinary))
	for _, text := range []string{ordinary, `res\`, "res"} {
		d := r.StreamDecoder()
		require.Equal(t, text, d.Feed(text)+d.Flush())
	}
}
