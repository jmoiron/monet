package gallery

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
)

const (
	base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	idLen  = 9 // 62^9 is about 1.3e16: plenty for a personal gallery
)

// NewID is a random photo id: 9 characters of base62.
func NewID() string {
	max := big.NewInt(int64(len(base62)))
	b := make([]byte, idLen)
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // the system's randomness is gone
		}
		b[i] = base62[n.Int64()]
	}
	return string(b)
}

// idFor is the same id for the same (namespace, name), so that importing the
// same thing again finds the photo it made before.
func idFor(namespace, name string) string {
	sum := sha1.Sum([]byte(namespace + "\x00" + name))
	return encodeID(binary.BigEndian.Uint64(sum[:8]))
}

// contentID is an id made from the bytes of a file: the same image is always
// the same id, wherever and whenever it is added. The whole file is hashed; it
// is all in memory already, and a prefix would not tell edits apart (the first
// 16KiB of a jpeg is mostly exif, which an edited copy keeps).
func contentID(data []byte) string {
	sum := sha256.Sum256(data)
	return encodeID(binary.BigEndian.Uint64(sum[:8]))
}

func encodeID(n uint64) string {
	b := make([]byte, idLen)
	for i := idLen - 1; i >= 0; i-- {
		b[i] = base62[n%62]
		n /= 62
	}
	return string(b)
}
