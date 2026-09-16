package database

import (
	"math"
	"testing"
)

func TestEncodeVectorLength(t *testing.T) {
	for _, dims := range []int{1, 8, 768, 1024} {
		vector := make([]float32, dims)
		if got, want := len(EncodeVector(vector)), dims*bytesPerFloat32; got != want {
			t.Errorf("EncodeVector(%d dims) produced %d bytes, want %d", dims, got, want)
		}
	}
}

func TestVectorRoundTrip(t *testing.T) {
	// Values chosen to exercise sign, zero, subnormal-ish magnitudes and the
	// kind of fractions a real embedding is made of.
	original := []float32{0, 1, -1, 0.5, -0.0625, 3.4e38, 1.2e-38, 0.017453292}

	decoded, err := DecodeVector(EncodeVector(original), len(original))
	if err != nil {
		t.Fatalf("DecodeVector: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("decoded %d components, want %d", len(decoded), len(original))
	}
	for i := range original {
		// Bit-for-bit, not approximately: the codec must not lose precision,
		// so comparing the float bits catches a silent float64 detour.
		if math.Float32bits(decoded[i]) != math.Float32bits(original[i]) {
			t.Errorf("component %d round-tripped as %v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestVectorNaNAndInfRoundTrip(t *testing.T) {
	original := []float32{
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
	}
	decoded, err := DecodeVector(EncodeVector(original), len(original))
	if err != nil {
		t.Fatalf("DecodeVector: %v", err)
	}
	if !math.IsNaN(float64(decoded[0])) {
		t.Errorf("NaN round-tripped as %v", decoded[0])
	}
	if !math.IsInf(float64(decoded[1]), 1) {
		t.Errorf("+Inf round-tripped as %v", decoded[1])
	}
	if !math.IsInf(float64(decoded[2]), -1) {
		t.Errorf("-Inf round-tripped as %v", decoded[2])
	}
}

// The on-disk byte order is a storage contract: every vector already written
// was encoded this way, so changing it silently invalidates them. Pin it with
// an explicit expected byte sequence rather than only a round-trip, which
// would pass just as happily on big-endian.
func TestEncodeVectorIsLittleEndian(t *testing.T) {
	// 1.0 as IEEE-754 float32 is 0x3F800000.
	got := EncodeVector([]float32{1})
	want := []byte{0x00, 0x00, 0x80, 0x3F}
	if len(got) != len(want) {
		t.Fatalf("got %d bytes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("encoded 1.0 as % x, want % x (little-endian float32)", got, want)
		}
	}
}

func TestDecodeVectorRejectsWrongLength(t *testing.T) {
	// A well-formed 4-component blob, then every way the length can be wrong.
	good := EncodeVector([]float32{1, 2, 3, 4})

	tests := []struct {
		name string
		blob []byte
		dims int
	}{
		{"one byte short", good[:len(good)-1], 4},
		{"one byte long", append(append([]byte(nil), good...), 0), 4},
		{"empty blob with positive dims", nil, 4},
		{"dims larger than blob", good, 5},
		{"dims smaller than blob", good, 3},
		{"zero dims", good, 0},
		{"negative dims", good, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeVector(tt.blob, tt.dims); err == nil {
				t.Fatalf("DecodeVector(%d bytes, dims=%d) succeeded, want an error: "+
					"a wrong-length blob must fail loudly rather than yield garbage similarity",
					len(tt.blob), tt.dims)
			}
		})
	}
}

func TestDotProduct(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical unit vectors", []float32{1, 0, 0}, []float32{1, 0, 0}, 1},
		{"orthogonal unit vectors", []float32{1, 0, 0}, []float32{0, 1, 0}, 0},
		{"opposed unit vectors", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1},
		{"half overlap", []float32{1, 0}, []float32{0.5, 0.5}, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DotProduct(tt.a, tt.b)
			if err != nil {
				t.Fatalf("DotProduct: %v", err)
			}
			if math.Abs(float64(got-tt.want)) > 1e-6 {
				t.Errorf("DotProduct(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDotProductRequiresEqualLength(t *testing.T) {
	// Mismatched widths can only mean a corrupt row or crossed models, so
	// returning a plausible-looking number would be worse than refusing. There
	// is no unchecked variant to reach for, by design.
	if _, err := DotProduct([]float32{1, 0}, []float32{1, 0, 0}); err == nil {
		t.Fatal("DotProduct accepted vectors of different widths, want an error")
	}
	if _, err := DotProduct([]float32{1, 0}, []float32{0, 1}); err != nil {
		t.Fatalf("DotProduct rejected equal-width vectors: %v", err)
	}
}
