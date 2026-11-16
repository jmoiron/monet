package geo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DumpURL is where GeoNames' files are. It is a variable for tests.
var DumpURL = "https://download.geonames.org/export/dump/"

// ErrNoData is that the files that Open reads aren't there: Fetch gets them.
var ErrNoData = errors.New("no geonames data")

const (
	citiesFile  = "cities1000.zip" // every city of a thousand people and over
	countryFile = "countryInfo.txt"
	adminFile   = "admin1CodesASCII.txt"
	alternates  = "alternateNamesV2.zip" // large, and only wanted to make localFile
	localFile   = "local-names.tsv"
)

// Present says if the data is in dir.
func Present(dir string) bool {
	for _, f := range []string{citiesFile, countryFile, adminFile, localFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// Open reads the data in dir, as Fetch left it.
func Open(dir string) (*Index, error) {
	if !Present(dir) {
		return nil, fmt.Errorf("%w in %s", ErrNoData, dir)
	}
	ix, err := Load(filepath.Join(dir, citiesFile), filepath.Join(dir, countryFile), filepath.Join(dir, adminFile))
	if err != nil {
		return nil, err
	}
	if err := ix.LoadLocalNames(filepath.Join(dir, localFile)); err != nil {
		return nil, err
	}
	return ix, nil
}

// Fetch gets the data from GeoNames into dir, and makes the file of the names
// in local languages from the alternate names, which are a few hundred
// megabytes and are not kept. It replaces what is there file by file, so that
// a run that fails leaves the data it found.
func Fetch(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range []string{citiesFile, countryFile, adminFile} {
		if err := download(ctx, DumpURL+f, filepath.Join(dir, f)); err != nil {
			return err
		}
	}
	alt := filepath.Join(dir, alternates)
	defer os.Remove(alt)
	if err := download(ctx, DumpURL+alternates, alt); err != nil {
		return err
	}
	ix, err := Load(filepath.Join(dir, citiesFile), filepath.Join(dir, countryFile), filepath.Join(dir, adminFile))
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, localFile+".part")
	defer os.Remove(tmp)
	if err := ix.LocalNames(alt, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, localFile))
}

// download fetches a url to a file, which only appears when it is all there.
func download(ctx context.Context, url, to string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "monet-geo")
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	part := to + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	defer os.Remove(part)
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", url, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, to)
}
