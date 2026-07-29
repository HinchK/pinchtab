package observe

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

// multiByteBody's cut offsets fall inside a 3-byte and a 4-byte character, so a
// raw byte cut orphans continuation bytes and the JSON encoder emits U+FFFD.
const multiByteBody = `{"note":"Docs — guide 🎯 done","tail":"padding padding padding"}`

func retainWithBody(t *testing.T, body string, base64Encoded bool, maxBytes int, perTab int64) NetworkEntry {
	t.Helper()

	orig := fetchResponseBody
	fetchResponseBody = func(context.Context, string) (string, bool, error) {
		return body, base64Encoded, nil
	}
	t.Cleanup(func() { fetchResponseBody = orig })

	nm := NewNetworkMonitor(16)
	nm.ConfigureBodyRetention(true, maxBytes)
	nm.retainBodyMaxPerTab = perTab

	buf := NewNetworkBuffer(16)
	buf.Add(NetworkEntry{RequestID: "req-1", URL: "https://example.test/", BodyPending: true})
	nm.maybeRetainBody(context.Background(), buf, "req-1")

	entry, ok := buf.Get("req-1")
	if !ok {
		t.Fatal("entry disappeared from the buffer")
	}
	return entry
}

func assertBodyIsCleanUTF8(t *testing.T, source, got string) {
	t.Helper()
	if !utf8.ValidString(got) {
		t.Fatalf("retained body is not valid UTF-8: %q", got)
	}
	if strings.Count(got, "�") > strings.Count(source, "�") {
		t.Fatalf("retained body gained replacement characters: %q", got)
	}
}

// The two cut sites are the opt-in retainBodyMaxBytes limit and the always-on
// per-tab budget; the second is reachable without opting in at all.
func TestRetainTextBodyCutsOnRuneBoundariesAtBothSites(t *testing.T) {
	for offset := 1; offset < len(multiByteBody); offset++ {
		byMaxBytes := retainWithBody(t, multiByteBody, false, offset, 1<<20)
		assertBodyIsCleanUTF8(t, multiByteBody, byMaxBytes.ResponseBody)
		if !byMaxBytes.BodyRetained {
			t.Fatalf("maxBytes=%d: text body must still be retained", offset)
		}

		byBudget := retainWithBody(t, multiByteBody, false, 0, int64(offset))
		assertBodyIsCleanUTF8(t, multiByteBody, byBudget.ResponseBody)
		if !byBudget.BodyRetained {
			t.Fatalf("budget=%d: text body must still be retained", offset)
		}
	}
}

func TestRetainTextBodyMarksTruncatedNotSkipped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		perTab int64
	}{
		{"retainBodyMaxBytes limit", 16, 1 << 20},
		{"per-tab remaining budget", 0, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := retainWithBody(t, multiByteBody, false, tc.limit, tc.perTab)

			if !entry.BodyTruncated {
				t.Fatalf("a cut-but-usable text body must report bodyTruncated: %+v", entry)
			}
			if entry.BodySkipped || entry.BodySkipReason != "" {
				t.Fatalf("a truncated text body must not also report bodySkipped: skipped=%v reason=%q", entry.BodySkipped, entry.BodySkipReason)
			}
			if entry.ResponseBody == "" {
				t.Fatal("a truncated text body must still carry the part that fit")
			}
		})
	}
}

func TestRetainBase64BodyOverBudgetIsSkippedNotCorrupted(t *testing.T) {
	raw := strings.Repeat("binary-payload-", 40)
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))

	for _, tc := range []struct {
		name       string
		limit      int
		perTab     int64
		wantReason string
	}{
		{"retainBodyMaxBytes limit", 32, 1 << 20, "base64 body exceeds retention limit"},
		{"per-tab remaining budget", 0, 32, "base64 body exceeds retention budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := retainWithBody(t, encoded, true, tc.limit, tc.perTab)

			if entry.ResponseBody != "" {
				if _, err := base64.StdEncoding.DecodeString(entry.ResponseBody); err != nil {
					t.Fatalf("returned an undecodable base64 fragment: %v (%q)", err, entry.ResponseBody)
				}
			}
			if !entry.BodySkipped {
				t.Fatalf("an over-budget base64 body must report bodySkipped: %+v", entry)
			}
			if entry.BodySkipReason != tc.wantReason {
				t.Fatalf("skip reason = %q, want %q", entry.BodySkipReason, tc.wantReason)
			}
			if entry.BodyTruncated {
				t.Fatal("a dropped body must not be reported as truncated: the client would parse a body that is not there")
			}
		})
	}
}

func TestRetainBodiesUnderBudgetAreUnchanged(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("small binary payload"))

	for _, tc := range []struct {
		name          string
		body          string
		base64Encoded bool
	}{
		{"text", multiByteBody, false},
		{"base64", encoded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := retainWithBody(t, tc.body, tc.base64Encoded, len(tc.body)+1, 1<<20)

			if entry.ResponseBody != tc.body {
				t.Fatalf("body under the budget was altered:\n got %q\nwant %q", entry.ResponseBody, tc.body)
			}
			if entry.BodyTruncated || entry.BodySkipped {
				t.Fatalf("body under the budget must be neither truncated nor skipped: %+v", entry)
			}
			if entry.Base64Encoded != tc.base64Encoded {
				t.Fatalf("base64Encoded = %v, want %v", entry.Base64Encoded, tc.base64Encoded)
			}
		})
	}
}
