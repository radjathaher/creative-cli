package broll

import (
	"net/url"
	"strings"
)

type videoFile struct {
	URL, MIME, Quality, Thumbnail string
	Width, Height                 int
}

func orientation(width, height int) string {
	switch {
	case width > height:
		return "landscape"
	case height > width:
		return "portrait"
	default:
		return "square"
	}
}

func selectVideo(files []videoFile, aspect, quality string) (videoFile, bool) {
	maxDimension := 1920
	switch quality {
	case "sd":
		maxDimension = 960
	case "4k":
		maxDimension = 4096
	case "best":
		maxDimension = 0
	}
	var best videoFile
	for _, f := range files {
		if f.Width <= 0 || f.Height <= 0 || !validURL(f.URL) {
			continue
		}
		if f.MIME != "" && f.MIME != "video/mp4" {
			continue
		}
		if f.MIME == "" {
			u, _ := url.Parse(f.URL)
			if !strings.HasSuffix(strings.ToLower(u.Path), ".mp4") {
				continue
			}
		}
		if aspect != "" && orientation(f.Width, f.Height) != aspect {
			continue
		}
		if maxDimension > 0 && max(f.Width, f.Height) > maxDimension {
			continue
		}
		if int64(f.Width)*int64(f.Height) > int64(best.Width)*int64(best.Height) {
			best = f
		}
	}
	return best, best.URL != ""
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
