package embed

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// Truncation is not tidiness, it is required. Measured against Ollama 0.32.0,
// qwen3-embedding:0.6b at num_ctx 8192 returns HTTP 400 "do embedding request:
// EOF" on long inputs, at an unstable threshold, while the same model at 2048
// accepts 200,000 characters. Relying on a provider's over-length behavior is
// therefore not safe, and a real corpus has items long enough to hit it -- the
// reference spool's largest is ~57,000 characters.
func TestEmbedTruncatesOverlongInput(t *testing.T) {
	f := newFakeOllama(t)
	// num_ctx 100 keeps the arithmetic easy: the cap is 100 * charsPerToken.
	p := newTestProvider(t, f, Config{NumCtx: 100})
	wantCap := 100 * charsPerToken

	long := strings.Repeat("a", wantCap*3)
	if _, err := p.Embed(context.Background(), []string{long}); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	requests, _ := f.recorded()
	sent := requests[0].Input[0]
	if len(sent) > wantCap {
		t.Errorf("sent %d chars, want at most %d", len(sent), wantCap)
	}
	if len(sent) == len(long) {
		t.Error("input was not truncated at all")
	}
}

// The prefix is part of what the model receives, so it must count against the
// cap rather than pushing the request past it.
func TestEmbedTruncationAccountsForThePrefix(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{Model: ModelNomicEmbedText, NumCtx: 100})
	wantCap := 100 * charsPerToken

	if _, err := p.Embed(context.Background(), []string{strings.Repeat("b", wantCap*2)}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, _ := f.recorded()
	sent := requests[0].Input[0]
	if len(sent) > wantCap {
		t.Errorf("prefix plus text is %d chars, want at most %d", len(sent), wantCap)
	}
	if !strings.HasPrefix(sent, nomicPrefix) {
		t.Error("truncation dropped the prefix, which the model needs to know the task")
	}
}

// Short inputs -- the overwhelming majority, at a ~100-token median on real
// data -- must pass through byte-for-byte.
func TestEmbedLeavesShortInputAlone(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{})

	const text = "a perfectly ordinary feed item #0"
	if _, err := p.Embed(context.Background(), []string{text}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, _ := f.recorded()
	if requests[0].Input[0] != text {
		t.Errorf("input = %q, want it unchanged", requests[0].Input[0])
	}
}

func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	// Multi-byte runes straddling the cap must not be split in half.
	s := strings.Repeat("é😀", 50)
	for limit := 1; limit < 40; limit++ {
		got := truncateRunes(s, limit)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateRunes(limit=%d) produced invalid UTF-8", limit)
		}
		if len(got) > limit {
			t.Fatalf("truncateRunes(limit=%d) returned %d bytes", limit, len(got))
		}
	}
}

// MaxInputChars has to track a configured num_ctx override, not just the
// model's default, since the cap exists to respect the context actually in use.
func TestMaxInputCharsFollowsConfiguredNumCtx(t *testing.T) {
	if got, want := (ModelDefaults{NumCtx: 2048}).MaxInputChars(), 2048*charsPerToken; got != want {
		t.Errorf("MaxInputChars() = %d, want %d", got, want)
	}

	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{Model: ModelNomicEmbedText, NumCtx: 512})
	if p.maxInputChars != 512*charsPerToken {
		t.Errorf("provider cap = %d, want %d from the configured num_ctx",
			p.maxInputChars, 512*charsPerToken)
	}
}
