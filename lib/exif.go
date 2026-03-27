// Copyright (c) 2021 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package lib

import (
	"fmt"
	"os/exec"
	"strings"
)

var exifStripExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".avif": true,
	".heif": true,
	".heic": true,
}

func runStripExifMetadata(filePath, ext string) error {
	if !exifStripExtensions[strings.ToLower(ext)] {
		return nil
	}
	out, err := exec.Command("exiftool",
		"-all=",              // clear all metadata (EXIF, IPTC, XMP, ICC profile, etc.)
		"-tagsfromfile", "@", // copy back tags from the same file (after clearing)
		"-exif:orientation",  // restore only the orientation tag
		"-P",                 // preserve file modification timestamp
		"-overwrite_original",
		"--",
		filePath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("exiftool: %w\n%s", err, out)
	}
	return nil
}
