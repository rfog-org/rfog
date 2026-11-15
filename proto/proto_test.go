package proto

import (
	"bytes"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := TurnStart{Match: "m1", Turn: 3, Deadline: time.Unix(100, 0).UTC()}
	if err := Encode(&buf, TTurnStart, in); err != nil {
		t.Fatal(err)
	}
	if err := Encode(&buf, TPing, nil); err != nil {
		t.Fatal(err)
	}
	f, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var out TurnStart
	if err := f.As(&out); err != nil {
		t.Fatal(err)
	}
	if out.Match != "m1" || out.Turn != 3 || !out.Deadline.Equal(in.Deadline) {
		t.Fatalf("got %+v", out)
	}
	f, err = Decode(&buf)
	if err != nil || f.T != TPing {
		t.Fatalf("ping: %v %+v", err, f)
	}
	if _, err := Decode(&buf); err == nil {
		t.Fatal("expected EOF")
	}
}

// A frame over the reader's limit is refused from its length alone,
// before its body is read or allocated.
func TestDecodeMax(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, TChat, Chat{Text: string(make([]byte, 100))}); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMax(bytes.NewReader(buf.Bytes()), 64); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err := DecodeMax(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff}), MaxClientFrame); err == nil {
		t.Fatal("4 GB length accepted")
	}
	if f, err := DecodeMax(&buf, MaxClientFrame); err != nil || f.T != TChat {
		t.Fatalf("frame under the limit: %v %+v", err, f)
	}
}
