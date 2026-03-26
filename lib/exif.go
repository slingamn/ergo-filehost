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
		"-exif:all=", // clear all EXIF tags (GPS, camera, timestamps, etc.)
		"-iptc:all=", // clear all IPTC tags (caption, keywords, copyright, etc.)
		"-xmp:all=",  // clear all XMP tags (Adobe metadata, Lightroom edits, etc.)
		"-P",         // preserve file modification timestamp
		"-overwrite_original",
		"--",
		filePath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("exiftool: %w\n%s", err, out)
	}
	return nil
}
