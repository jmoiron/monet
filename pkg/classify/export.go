package classify

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// What a gallery is sent to learn what was seen in its photos: the output of
// `classify export`, and what the gallery's admin imports. It is a file, so that
// the machine that looks at the photos and the machine that serves them don't
// have to be the same one.

// ExportVersion is the version of the file format.
const ExportVersion = 1

// An Export is a set of classifications of the photos of one gallery.
type Export struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Items   []Item    `json:"items"`
}

// An Item is what one model saw in one photo. A photo has an item per model.
type Item struct {
	ID     string `json:"id"`     // the gallery's id of the photo
	SHA256 string `json:"sha256"` // of the photo's original, so that a stale item is not put on another image
	Model  string `json:"model"`  // the backend's name: what its results are called

	Caption string  `json:"caption,omitempty"`
	Labels  []Label `json:"labels,omitempty"`

	// Embedding is little-endian float16s, in base64: half the size of float32s,
	// and as much precision as comparing photos can use.
	Embedding string `json:"embedding,omitempty"`
}

// EncodeEmbedding is v as an Item holds it.
func EncodeEmbedding(v []float32) string {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], float16(x))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeEmbedding is the inverse of EncodeEmbedding.
func DecodeEmbedding(s string) ([]float32, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return DecodeHalves(b)
}

// DecodeHalves reads little-endian float16s.
func DecodeHalves(b []byte) ([]float32, error) {
	if len(b)%2 != 0 {
		return nil, fmt.Errorf("an embedding of %d bytes is not float16s", len(b))
	}
	v := make([]float32, len(b)/2)
	for i := range v {
		v[i] = float32From16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return v, nil
}

// float16 rounds x to an IEEE 754 half (to nearest, ties to even).
func float16(x float32) uint16 {
	b := math.Float32bits(x)
	sign := uint16(b>>16) & 0x8000
	exp := int((b>>23)&0xff) - 127 + 15
	mant := b & 0x7fffff
	switch {
	case (b>>23)&0xff == 0xff: // inf or nan
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 0x1f: // too big
		return sign | 0x7c00
	case exp <= 0: // subnormal, or too small
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint(14 - exp)
		half := uint16(mant >> shift)
		rem := mant & (1<<shift - 1)
		if mid := uint32(1) << (shift - 1); rem > mid || (rem == mid && half&1 == 1) {
			half++
		}
		return sign | half
	}
	half := sign | uint16(exp)<<10 | uint16(mant>>13)
	if rem := mant & 0x1fff; rem > 0x1000 || (rem == 0x1000 && half&1 == 1) {
		half++ // may carry into the exponent, which is right
	}
	return half
}

func float32From16(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch {
	case exp == 0 && mant == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal: normalise it
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3ff)<<13)
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}

// Normalize returns v scaled to unit length, or nil if it has none.
func Normalize(v []float32) []float32 { return normalized(v) }
