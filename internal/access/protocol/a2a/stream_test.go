package a2a

import (
	"testing"
)

func TestEncodeStreamChunkRoundTrips(t *testing.T) {
	data, err := EncodeStreamChunk("hello delta")
	if err != nil {
		t.Fatal(err)
	}
	delta, completed, err := ParseStreamFrame(data)
	if err != nil || completed || delta != "hello delta" {
		t.Fatalf("ParseStreamFrame(%s) = %q, %v, %v", data, delta, completed, err)
	}
}

func TestEncodeStreamCompletedParsesTerminal(t *testing.T) {
	data, err := EncodeStreamCompleted()
	if err != nil {
		t.Fatal(err)
	}
	delta, completed, err := ParseStreamFrame(data)
	if err != nil || !completed || delta != "" {
		t.Fatalf("ParseStreamFrame(%s) = %q, %v, %v", data, delta, completed, err)
	}
}

func TestParseStreamFrameConcatenatesTextParts(t *testing.T) {
	delta, completed, err := ParseStreamFrame([]byte(`{"type":"message","message":{"parts":[{"kind":"text","text":"a"},{"kind":"text","text":"b"},{"kind":"file","text":"ignored"}]}}`))
	if err != nil || completed || delta != "a\nb" {
		t.Fatalf("ParseStreamFrame() = %q, %v, %v", delta, completed, err)
	}
}

func TestParseStreamFrameSurfacesJSONRPCError(t *testing.T) {
	_, _, err := ParseStreamFrame([]byte(`{"jsonrpc":"2.0","id":"1","error":{"code":-32000,"message":"remote boom"}}`))
	if err == nil || err.Error() == "" {
		t.Fatalf("expected a JSON-RPC stream error, got %v", err)
	}
	if !contains(err.Error(), "remote boom") {
		t.Fatalf("error must carry the peer message: %v", err)
	}
}

func TestParseStreamFrameReportsMalformedJSON(t *testing.T) {
	if _, _, err := ParseStreamFrame([]byte(`not json`)); err == nil {
		t.Fatal("malformed frame must be an error")
	}
}

func TestParseStreamFrameIgnoresUnknownOrEmpty(t *testing.T) {
	for _, raw := range []string{
		``,
		`   `,
		`{"type":"ping"}`,
		`{"type":"output_text.delta","text":"not-our-profile"}`,
	} {
		delta, completed, err := ParseStreamFrame([]byte(raw))
		if err != nil {
			t.Fatalf("frame %q must not error: %v", raw, err)
		}
		if delta != "" || completed {
			t.Fatalf("unknown frame %q parsed as delta=%q completed=%v", raw, delta, completed)
		}
	}
}

func contains(value, substring string) bool {
	return len(value) >= len(substring) && (value == substring || indexOf(value, substring) >= 0)
}

func indexOf(value, substring string) int {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return i
		}
	}
	return -1
}
