package bookmarks

import (
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// what an older gowitness prints when it fails: its usage text, then the error
// in a padded box; on the reporter's server the error text was empty
const usageDump = "Usage:\n  gowitness scan single [flags]\n\nFlags:\n  -h, --help   help for single\n\n\n"

func box(msg string) string {
	pad := func(s string) string { return "  " + s + strings.Repeat(" ", 76-len(s)) + "\n" }
	return usageDump + pad("An error occured running the single command") + pad("The error was:") + pad(msg) + "\n\n"
}

func TestCommandFailureReason(t *testing.T) {
	exit1 := errors.New("exit status 1")

	// the server log from the bug report: usage text and a blank error
	r := commandFailureReason([]byte(box("")), exit1)
	assert.Contains(t, r, "exit status 1")
	assert.Contains(t, r, "no error text")
	assert.Contains(t, r, "-D")
	assert.NotContains(t, r, "Usage", "the usage dump is not the reason")

	// newer versions say what went wrong, wrapped across padded lines
	out := usageDump + "  An error occured running the single command   \n  The error was:   \n" +
		"  failed to initialize chrome context: fork/exec /nonexistent/chrome: no such   \n  file or directory   \n\n\n"
	assert.Equal(t, "failed to initialize chrome context: fork/exec /nonexistent/chrome: no such file or directory",
		commandFailureReason([]byte(out), exit1))

	// no usage report, but gowitness logged its own error
	logged := "2026/10/14 23:42:24 ERRO failed to witness target target=https://x/ err=\"page load error net::ERR_NAME_NOT_RESOLVED\"\n"
	assert.Equal(t, "page load error net::ERR_NAME_NOT_RESOLVED", commandFailureReason([]byte(logged), exit1))

	// nothing at all: say what we do know
	assert.Equal(t, "exit status 2", commandFailureReason(nil, errors.New("exit status 2")))
	assert.Contains(t, commandFailureReason(nil, errors.New("signal: killed")), "killed")
}

func TestTailText(t *testing.T) {
	assert.Equal(t, "short", tailText([]byte("  short \n"), 100))
	assert.Equal(t, "...cdefgh", tailText([]byte("abcdefgh"), 6))
}

// a stand-in gowitness: behavior per driver comes from the environment, and it
// records which drivers were tried
func fakeGowitness(t *testing.T, chromedp, gorod string) (bin, driversFile string) {
	t.Helper()
	dir := t.TempDir()

	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	for x := 0; x < 64; x++ {
		for y := 0; y < 40; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 6), 120, 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "page.jpg"))
	assert.NoError(t, err)
	assert.NoError(t, jpeg.Encode(f, img, nil))
	f.Close()

	driversFile = filepath.Join(dir, "drivers")
	script := `#!/bin/sh
driver=""; shot=""; json=""
while [ $# -gt 0 ]; do
  case "$1" in
    --driver) driver="$2"; shift;;
    --screenshot-path) shot="$2"; shift;;
    --write-jsonl-file) json="$2"; shift;;
  esac
  shift
done
echo "$driver" >> '` + driversFile + `'
case "$driver" in
  chromedp) mode='` + chromedp + `';;
  gorod) mode='` + gorod + `';;
esac
case "$mode" in
  ok)
    cp '` + filepath.Join(dir, "page.jpg") + `' "$shot/page.jpeg"
    echo '{"url":"x","title":"A Title"}' > "$json"
    exit 0;;
  nothing)
    echo '{"url":"x","failed":true,"failed_reason":"context deadline exceeded"}' > "$json"
    exit 0;;
  crash-blank)
    printf 'Usage:\n  gowitness scan single [flags]\n\n  An error occured running the single command\n  The error was:\n\n\n\n'
    exit 1;;
  crash-msg)
    printf 'Usage:\n  gowitness scan single [flags]\n\n  An error occured running the single command\n  The error was:\n  no chrome here\n\n'
    exit 1;;
esac
exit 3
`
	bin = filepath.Join(dir, "gowitness")
	assert.NoError(t, os.WriteFile(bin, []byte(script), 0755))
	return bin, driversFile
}

func tried(t *testing.T, driversFile string) []string {
	data, err := os.ReadFile(driversFile)
	assert.NoError(t, err)
	return strings.Fields(string(data))
}

func TestScreenshotFallsBackWhenFirstDriverCrashes(t *testing.T) {
	// the bug report: a driver exiting non-zero used to end the attempt
	bin, drivers := fakeGowitness(t, "crash-msg", "ok")
	s := NewScreenshotService(t.TempDir(), bin, true)
	res, err := s.TakeScreenshot("https://example.com", "bm1")
	assert.NoError(t, err)
	if assert.NotNil(t, res) {
		assert.FileExists(t, res.ScreenshotPath)
		assert.Equal(t, "A Title", res.Title)
	}
	assert.Equal(t, []string{"chromedp", "gorod"}, tried(t, drivers))
}

func TestScreenshotFallsBackWhenFirstDriverFindsNothing(t *testing.T) {
	bin, drivers := fakeGowitness(t, "nothing", "ok")
	s := NewScreenshotService(t.TempDir(), bin, true)
	res, err := s.TakeScreenshot("https://example.com", "bm1")
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, []string{"chromedp", "gorod"}, tried(t, drivers))
}

func TestScreenshotFirstDriverSuccessSkipsFallback(t *testing.T) {
	bin, drivers := fakeGowitness(t, "ok", "crash-msg")
	s := NewScreenshotService(t.TempDir(), bin, true)
	_, err := s.TakeScreenshot("https://example.com", "bm1")
	assert.NoError(t, err)
	assert.Equal(t, []string{"chromedp"}, tried(t, drivers))
}

func TestScreenshotErrorExplainsEachDriver(t *testing.T) {
	dir := t.TempDir()
	bin, drivers := fakeGowitness(t, "nothing", "crash-blank")
	s := NewScreenshotService(dir, bin, true)
	res, err := s.TakeScreenshot("https://example.com", "bm1")
	assert.Nil(t, res)
	if assert.Error(t, err) {
		msg := err.Error()
		assert.Contains(t, msg, "chromedp: context deadline exceeded")
		assert.Contains(t, msg, "gorod: exit status 1")
		assert.Contains(t, msg, "no error text")
		assert.NotContains(t, msg, "Usage", "the usage dump doesn't go to the user")
	}
	assert.Equal(t, []string{"chromedp", "gorod"}, tried(t, drivers))

	// no orphaned files for a bookmark that got no screenshot
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), "bm1", "left behind: %s", e.Name())
	}
}

// a gowitness that records its arguments, one run per line, and fails
func argRecorder(t *testing.T) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" >> '" + argsFile + "'\nexit 1\n"
	bin = filepath.Join(dir, "gowitness")
	assert.NoError(t, os.WriteFile(bin, []byte(script), 0755))
	return bin, argsFile
}

func TestChromePathIsPassedToBothDrivers(t *testing.T) {
	bin, argsFile := argRecorder(t)
	s := NewScreenshotService(t.TempDir(), bin, true).WithChromePath("/opt/monet/chrome-nosandbox")
	_, err := s.TakeScreenshot("https://example.com", "bm1")
	assert.Error(t, err)

	data, _ := os.ReadFile(argsFile)
	runs := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, runs, 2, "both drivers are tried")
	for _, run := range runs {
		assert.Contains(t, run, "--chrome-path /opt/monet/chrome-nosandbox")
	}
}

func TestNoChromePathFlagWhenNotConfigured(t *testing.T) {
	bin, argsFile := argRecorder(t)
	s := NewScreenshotService(t.TempDir(), bin, true)
	_, err := s.TakeScreenshot("https://example.com", "bm1")
	assert.Error(t, err)

	data, _ := os.ReadFile(argsFile)
	assert.NotContains(t, string(data), "--chrome-path", "gowitness picks its own browser")
	assert.Contains(t, string(data), "--driver chromedp")
}
