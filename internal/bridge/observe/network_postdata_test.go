package observe

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chromedp/cdproto/network"
)

func postDataEntries(chunks ...string) []*network.PostDataEntry {
	entries := make([]*network.PostDataEntry, 0, len(chunks))
	for _, chunk := range chunks {
		entries = append(entries, &network.PostDataEntry{Bytes: base64.StdEncoding.EncodeToString([]byte(chunk))})
	}
	return entries
}

// A caller reading a field named after the request body gets the bytes the page sent. CDP
// hands them over base64-encoded, so publishing the entries as they arrive puts an encoded
// blob in a text field with nothing on the entry saying so.
func TestPostDataIsTheBodyThePageSentNotBase64(t *testing.T) {
	const body = `{"hi":"there — 🎯"}`

	got := decodePostData(postDataEntries(body))

	if got != body {
		t.Errorf("postData = %q, want the body byte for byte", got)
	}
	if _, err := base64.StdEncoding.DecodeString(got); err == nil && got != "" {
		t.Errorf("postData %q still decodes as base64, so it is very likely still encoded", got)
	}
}

// The case string concatenation corrupts: joining per-entry base64 is only equal to the
// base64 of the joined bytes when every chunk's length is a multiple of three, because the
// padding of the earlier chunk otherwise lands mid-stream. Chrome splits large and multipart
// bodies into entries, so this is the ordinary shape rather than an edge case.
func TestPostDataJoinsMultipleEntriesOnDecodedBytes(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"first chunk length 1 mod 3", "a", `{"k":"v"}`},
		{"first chunk length 2 mod 3", "ab", `{"k":"v"}`},
		{"first chunk length 0 mod 3", "abc", `{"k":"v"}`},
		{"multi-byte across the boundary", "héllo", " wörld 🎯"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.first + tc.second

			got := decodePostData(postDataEntries(tc.first, tc.second))

			if got != want {
				t.Errorf("postData = %q, want %q — the entries were joined before decoding", got, want)
			}
			if len(tc.first)%3 == 0 {
				return
			}
			concatenated := base64.StdEncoding.EncodeToString([]byte(tc.first)) + base64.StdEncoding.EncodeToString([]byte(tc.second))
			if _, err := base64.StdEncoding.DecodeString(concatenated); err == nil {
				t.Errorf("fixture is not exercising the corruption: %q decodes cleanly, so a string join would have been harmless", concatenated)
			}
		})
	}
}

// Nothing on the entry says what encoding postData carries, so a value that cannot be
// published as the text the field claims to be is not published at all. PIN-114 owns the
// signal that says why it is absent.
func TestPostDataPublishesNothingRatherThanSomethingItCannotDescribe(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*network.PostDataEntry
	}{
		{
			name:    "not base64 at all",
			entries: []*network.PostDataEntry{{Bytes: "not base64!!"}},
		},
		{
			name:    "one bad entry among good ones",
			entries: append(postDataEntries("good"), &network.PostDataEntry{Bytes: "@@@"}),
		},
		{
			name:    "decodes to bytes that are not valid UTF-8",
			entries: []*network.PostDataEntry{{Bytes: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x00, 0x80})}},
		},
		{
			name:    "valid UTF-8 chunks that are invalid once joined",
			entries: []*network.PostDataEntry{{Bytes: base64.StdEncoding.EncodeToString([]byte("é")[:1])}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decodePostData(tc.entries)

			if got != "" {
				t.Errorf("postData = %q, want it absent: the field carries no encoding signal, so this is either mojibake or a blob claiming to be text", got)
			}
		})
	}
}

func TestPostDataIsAbsentWhenTheRequestHasNoBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*network.PostDataEntry
	}{
		{name: "no entries", entries: nil},
		{name: "empty entry list", entries: []*network.PostDataEntry{}},
		{name: "nil entry", entries: []*network.PostDataEntry{nil}},
		{name: "empty entry", entries: postDataEntries("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodePostData(tc.entries); got != "" {
				t.Errorf("postData = %q, want empty", got)
			}
		})
	}
}

// The cap now measures the DECODED body, so the constant describes request content rather
// than roughly three quarters of it. Driven at every offset around a multi-byte rune that
// straddles the limit, because a cut inside one is what the encoded value used to hide: the
// old rule cut base64 text, where every byte is ASCII and no cut ever looked wrong.
func TestPostDataCapMeasuresTheDecodedBodyAndCutsOnARuneBoundary(t *testing.T) {
	const tail = "— 🎯 done"
	body := strings.Repeat("a", maxNetworkPostDataBytes-4) + tail

	if len(body) <= maxNetworkPostDataBytes {
		t.Fatalf("fixture is %d bytes, must exceed the %d-byte cap or nothing is cut", len(body), maxNetworkPostDataBytes)
	}
	if utf8.RuneLen([]rune(tail)[0]) == 1 {
		t.Fatal("fixture must straddle the limit with a multi-byte rune")
	}

	entry := normalizeNetworkEntry(NetworkEntry{PostData: decodePostData(postDataEntries(body))})

	if len(entry.PostData) > maxNetworkPostDataBytes {
		t.Errorf("postData is %d bytes, over the %d-byte cap on the decoded body", len(entry.PostData), maxNetworkPostDataBytes)
	}
	if !utf8.ValidString(entry.PostData) {
		t.Error("postData was cut inside a rune, so the body no longer decodes as the text it claims to be")
	}
	if entry.PostData != body[:len(entry.PostData)] {
		t.Error("postData is not a byte-exact prefix of the body the page sent")
	}
	if strings.Count(entry.PostData, "�") > strings.Count(body, "�") {
		t.Error("postData gained replacement characters absent from the body")
	}
}

// HAR defines postData.text as the request body, and its only encoding declaration lives on
// response content — so a request body that is not text has no honest place in a HAR entry at
// all. Publishing the decoded body is what makes the field mean what the format says.
func TestHARPostDataTextHoldsTheDecodedBody(t *testing.T) {
	const body = `{"hi":"there — 🎯"}`

	entry := NetworkEntry{
		URL:            "https://example.test/sink",
		Method:         "POST",
		PostData:       decodePostData(postDataEntries(body)),
		RequestHeaders: map[string]string{"Content-Type": "application/json"},
	}

	e := NetworkEntryToExport(entry, "", false)

	if e.Request.PostData == nil {
		t.Fatal("HAR entry carries no postData for a request that had a body")
	}
	if e.Request.PostData.Text != body {
		t.Errorf("postData.text = %q, want the body the page sent — HAR defines this field as the plain body", e.Request.PostData.Text)
	}
	if e.Request.BodySize != len(body) {
		t.Errorf("request bodySize = %d, want the decoded length %d", e.Request.BodySize, len(body))
	}
}

// A body this package refuses to publish must not reappear in the export as an empty text
// block that reads like a request sent with no body at all.
func TestHAROmitsPostDataWhenTheBodyCouldNotBePublished(t *testing.T) {
	entry := NetworkEntry{
		URL:      "https://example.test/upload",
		Method:   "POST",
		PostData: decodePostData([]*network.PostDataEntry{{Bytes: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})}}),
	}

	if e := NetworkEntryToExport(entry, "", false); e.Request.PostData != nil {
		t.Errorf("postData = %+v, want it omitted rather than an empty text block", e.Request.PostData)
	}
}

// A body under the cap is published whole: the cap must not be measuring the encoded length,
// where a body between three quarters of the limit and the limit would still be cut.
func TestPostDataUnderTheDecodedCapIsNotCut(t *testing.T) {
	body := strings.Repeat("b", maxNetworkPostDataBytes-1)

	if encoded := base64.StdEncoding.EncodeToString([]byte(body)); len(encoded) <= maxNetworkPostDataBytes {
		t.Fatalf("fixture encodes to %d bytes, which is under the cap too — it cannot tell the two measurements apart", len(encoded))
	}

	if got := normalizeNetworkEntry(NetworkEntry{PostData: decodePostData(postDataEntries(body))}).PostData; got != body {
		t.Errorf("postData is %d bytes, want the whole %d-byte body: the cap is measuring the encoded length", len(got), len(body))
	}
}
