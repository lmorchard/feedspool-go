package itemtext

import (
	"testing"
	"unicode/utf8"
)

// plainTextInput and lessThanInput pass through Derive unchanged, so they
// appear as both the input and the expected output in the table below.
// beforeAfterWant is the shared expected output of the two well-formed
// raw-text regression cases below.
const (
	plainTextInput  = "just plain text, nothing fancy"
	lessThanInput   = "a < b and b > a"
	beforeAfterWant = "before after"
)

func TestDeriveStripsMarkup(t *testing.T) {
	got := Derive(
		"Rust 1.87 &amp; friends",
		"<p>A <b>short</b> summary</p>",
		`<div>Hello <script>var x = "networking";</script> world</div>`,
		DefaultOptions(),
	)
	if got.Title != "Rust 1.87 & friends" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Summary != "A short summary" {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Body != "Hello world" {
		t.Errorf("body = %q, script contents must not be indexed", got.Body)
	}
}

func TestDeriveTruncatesOnRuneBoundary(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxBodyBytes = 5
	got := Derive("", "", "aé😀bcdef", opts)
	if !utf8.ValidString(got.Body) {
		t.Fatalf("body %q is not valid UTF-8", got.Body)
	}
	if len(got.Body) > opts.MaxBodyBytes {
		t.Fatalf("body is %d bytes, cap is %d", len(got.Body), opts.MaxBodyBytes)
	}
}

func TestSourceHashDistinguishesFieldBoundaries(t *testing.T) {
	// Without a separator these two would hash identically.
	if SourceHash("ab", "c", "") == SourceHash("a", "bc", "") {
		t.Fatal("hash ignores the boundary between title and summary")
	}
	first := SourceHash("a", "b", "c")
	second := SourceHash("a", "b", "c")
	if first != second {
		t.Fatal("hash is not stable")
	}
}

func TestDeriveBodyTableDriven(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text with no markup passes through",
			input: plainTextInput,
			want:  plainTextInput,
		},
		{
			name:  "a less-than b survives as text",
			input: lessThanInput,
			want:  lessThanInput,
		},
		{
			name:  "entity-only input decodes",
			input: "&amp;&lt;&gt;",
			want:  "&<>",
		},
		{
			name:  "empty input yields empty output",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace and newlines collapse to single spaces",
			input: "line one\n\n  line   two\ttabbed",
			want:  "line one line two tabbed",
		},
		{
			name:  "unclosed tags do not consume the rest of the document",
			input: "<div>before<b>bold text after",
			want:  "before bold text after",
		},
		{
			name:  "unclosed script does not consume trailing text",
			input: "<div>before<script>var x = 1; after script text but no closing tag",
			want:  "before var x = 1; after script text but no closing tag",
		},
		{
			name:  "unclosed style does not consume trailing text",
			input: "<div>before<style>.a{color:red} after style text but no closing tag",
			want:  "before .a{color:red} after style text but no closing tag",
		},
		{
			name:  "well-formed script contents are still dropped",
			input: "<div>before<script>var x = 1;</script>after</div>",
			want:  beforeAfterWant,
		},
		{
			name:  "well-formed style contents are still dropped",
			input: "<div>before<style>.a{color:red}</style>after</div>",
			want:  beforeAfterWant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Derive("", "", tt.input, DefaultOptions())
			if got.Body != tt.want {
				t.Errorf("Derive body = %q, want %q", got.Body, tt.want)
			}
		})
	}
}
