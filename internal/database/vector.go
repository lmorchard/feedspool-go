package database

import (
	"encoding/binary"
	"fmt"
	"math"
)

// bytesPerFloat32 is the on-disk width of one vector component.
const bytesPerFloat32 = 4

// EncodeVector packs a vector as little-endian float32, dims*4 bytes.
//
// float32 rather than float64 halves the space with no measurable effect on
// similarity, and no quantization: at a few thousand vectors per window that
// would be complexity bought for nothing.
//
// The byte order is a storage contract. Every vector already written was
// encoded this way, so changing it invalidates them all -- which is why
// TestEncodeVectorIsLittleEndian pins the exact bytes rather than only
// round-tripping.
func EncodeVector(vector []float32) []byte {
	blob := make([]byte, len(vector)*bytesPerFloat32)
	for i, component := range vector {
		binary.LittleEndian.PutUint32(
			blob[i*bytesPerFloat32:], math.Float32bits(component),
		)
	}
	return blob
}

// DecodeVector unpacks a blob written by EncodeVector.
//
// It errors unless the blob is exactly dims*4 bytes. A length disagreement can
// only mean corruption or a crossed model, and a wrong-width vector would
// otherwise decode into a plausible-looking number that silently poisons every
// similarity it takes part in.
func DecodeVector(blob []byte, dims int) ([]float32, error) {
	if dims <= 0 {
		return nil, fmt.Errorf("embedding dims is %d, want a positive count", dims)
	}
	if want := dims * bytesPerFloat32; len(blob) != want {
		return nil, fmt.Errorf(
			"embedding blob is %d bytes but %d dims needs exactly %d: the stored vector "+
				"does not match its dims column", len(blob), dims, want,
		)
	}

	vector := make([]float32, dims)
	for i := range vector {
		vector[i] = math.Float32frombits(
			binary.LittleEndian.Uint32(blob[i*bytesPerFloat32:]),
		)
	}
	return vector, nil
}

// DotProduct is the similarity between two vectors.
//
// For L2-normalized vectors the dot product IS cosine similarity, which is why
// nothing here normalizes: internal/embed verifies at the provider that the
// model returns unit-length vectors, so the division cosine would need is
// always by one.
//
// The width check is not optional and there is deliberately no unchecked
// variant. Every caller compares vectors read from separate rows, where a
// mismatch means corruption or crossed models -- and an unchecked loop over
// the longer slice would read out of bounds.
func DotProduct(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf(
			"cannot compare a %d-dimension vector with a %d-dimension one", len(a), len(b),
		)
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum, nil
}
