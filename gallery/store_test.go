package gallery

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreRefusesPathsOutside(t *testing.T) {
	root := filepath.Join(t.TempDir(), "photos")
	s, err := NewStore(root)
	require.NoError(t, err)

	for _, bad := range []string{"", "../evil", "a/../../evil", "/etc/passwd"} {
		assert.Error(t, s.Write(bad, []byte("x")), bad)
		if bad != "" { // a blank to remove is skipped, not refused
			assert.Error(t, s.Remove(bad), bad)
		}
		assert.False(t, s.Exists(bad), bad)
	}
	assert.NoError(t, s.Write("a/b/c.txt", []byte("x")))
	assert.True(t, s.Exists("a/b/c.txt"))
	got, err := s.Read("a/b/c.txt")
	assert.NoError(t, err)
	assert.Equal(t, "x", string(got))

	// removing what's gone, or nothing, is fine
	assert.NoError(t, s.Remove("a/b/c.txt", "a/b/c.txt", ""))
	assert.False(t, s.Exists("a/b/c.txt"))
}

func TestLookAtMakesWebSizedCopies(t *testing.T) {
	img, err := LookAt(testJPEG(t, 3000, 2000), 1)
	require.NoError(t, err)
	assert.Equal(t, "jpeg", img.Format)
	assert.Equal(t, ".jpg", img.Ext)
	assert.Equal(t, [2]int{3000, 2000}, [2]int{img.Width, img.Height})

	w, h := imageSize(t, img.Large)
	assert.Equal(t, [2]int{1920, 1280}, [2]int{w, h})
	w, h = imageSize(t, img.Thumb)
	assert.Equal(t, [2]int{800, 533}, [2]int{w, h})
}

func TestLookAtLeavesSmallImagesAlone(t *testing.T) {
	img, err := LookAt(testJPEG(t, 640, 480), 1)
	require.NoError(t, err)
	assert.Nil(t, img.Large, "the original is already small enough to show")
	w, h := imageSize(t, img.Thumb)
	assert.Equal(t, [2]int{640, 480}, [2]int{w, h}, "and is never scaled up")
}

func TestLookAtAppliesOrientation(t *testing.T) {
	// stored 400x200 but meant to be shown turned a quarter: 200 wide, 400 tall
	img, err := LookAt(testJPEG(t, 400, 200), 6)
	require.NoError(t, err)
	assert.Equal(t, [2]int{200, 400}, [2]int{img.Width, img.Height})
	w, h := imageSize(t, img.Thumb)
	assert.Equal(t, [2]int{200, 400}, [2]int{w, h})

	// a big one is turned in its web copy, too
	img, err = LookAt(testJPEG(t, 3000, 2000), 8)
	require.NoError(t, err)
	w, h = imageSize(t, img.Large)
	assert.Equal(t, [2]int{1280, 1920}, [2]int{w, h})
}

func TestLookAtFormats(t *testing.T) {
	img, err := LookAt(tinyPNG(t), 1)
	require.NoError(t, err)
	assert.Equal(t, [2]string{"png", "image/png"}, [2]string{img.Format, img.Mime})
	assert.NotNil(t, img.Thumb)

	_, err = LookAt([]byte("hello"), 1)
	assert.Error(t, err)
}
