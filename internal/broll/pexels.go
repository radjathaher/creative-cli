package broll

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type pexelsVideo struct {
	ID       int64   `json:"id"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration"`
	URL      string  `json:"url"`
	Image    string  `json:"image"`
	User     struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"user"`
	VideoFiles []struct {
		Quality  string `json:"quality"`
		FileType string `json:"file_type"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Link     string `json:"link"`
	} `json:"video_files"`
}

func (c *Client) searchPexels(ctx context.Context, o SearchOptions) ([]Result, error) {
	headers, err := c.pexelsHeaders()
	if err != nil {
		return nil, err
	}
	limit := o.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 80 {
		limit = 80
	}
	params := url.Values{"query": {o.Query}, "per_page": {strconv.Itoa(limit)}}
	if o.Orientation != "" {
		params.Set("orientation", o.Orientation)
	}
	var payload struct {
		Videos []pexelsVideo `json:"videos"`
	}
	endpoint := strings.TrimRight(c.PexelsURL, "/") + "/search?" + params.Encode()
	if err := c.getJSON(ctx, "pexels", endpoint, headers, &payload); err != nil {
		return nil, err
	}
	if payload.Videos == nil {
		return nil, errors.New("pexels: response does not contain a videos array")
	}
	results := make([]Result, 0, len(payload.Videos))
	for _, video := range payload.Videos {
		if result, ok := video.result(o.Orientation, o.Quality); ok {
			result.Query = o.Query
			results = append(results, result)
		}
	}
	return results, nil
}

func (c *Client) getPexels(ctx context.Context, id, quality string) (Result, error) {
	expectedID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || expectedID <= 0 {
		return Result{}, errors.New("pexels: invalid video ID")
	}
	headers, err := c.pexelsHeaders()
	if err != nil {
		return Result{}, err
	}
	var video pexelsVideo
	endpoint := strings.TrimRight(c.PexelsURL, "/") + "/videos/" + strconv.FormatInt(expectedID, 10)
	if err := c.getJSON(ctx, "pexels", endpoint, headers, &video); err != nil {
		return Result{}, err
	}
	if video.ID != expectedID {
		return Result{}, errors.New("pexels: response video ID does not match the requested video")
	}
	if video.Duration <= 0 || !validURL(video.URL) {
		return Result{}, errors.New("pexels: video duration or source page URL is missing or invalid")
	}
	result, ok := video.result("", quality)
	if !ok {
		return Result{}, errors.New("pexels: no playable MP4 variant matches the requested quality")
	}
	return result, nil
}

func (c *Client) pexelsHeaders() (map[string]string, error) {
	key, err := c.ReadSecret("PEXELS_API_KEY")
	if err != nil || strings.TrimSpace(key) == "" {
		return nil, errors.New("pexels: PEXELS_API_KEY is not configured")
	}
	return map[string]string{"Authorization": key}, nil
}

func (v pexelsVideo) result(wantedOrientation, quality string) (Result, bool) {
	if v.ID <= 0 || v.Duration <= 0 || !validURL(v.URL) {
		return Result{}, false
	}
	files := make([]videoFile, 0, len(v.VideoFiles))
	for _, file := range v.VideoFiles {
		if strings.EqualFold(strings.TrimSpace(file.FileType), "video/mp4") {
			files = append(files, videoFile{
				URL: file.Link, MIME: "video/mp4", Quality: file.Quality,
				Width: file.Width, Height: file.Height,
			})
		}
	}
	file, ok := selectVideo(files, wantedOrientation, quality)
	if !ok {
		return Result{}, false
	}
	return Result{
		ID: "pexels:" + strconv.FormatInt(v.ID, 10), Provider: "pexels", Title: v.title(),
		DurationS: v.Duration, Width: file.Width, Height: file.Height,
		Orientation: orientation(file.Width, file.Height), PreviewURL: file.URL,
		ThumbnailURL: v.Image, PageURL: v.URL, DownloadURL: file.URL,
		Author: v.User.Name, AuthorURL: v.User.URL,
		Licence: "Pexels License", LicenceURL: "https://www.pexels.com/license/", CostUSD: 0,
	}, true
}

func (v pexelsVideo) title() string {
	id := strconv.FormatInt(v.ID, 10)
	u, err := url.Parse(v.URL)
	if err == nil && (u.Hostname() == "www.pexels.com" || u.Hostname() == "pexels.com") {
		slug := path.Base(strings.TrimRight(u.Path, "/"))
		if strings.HasSuffix(slug, "-"+id) {
			title := strings.ReplaceAll(strings.TrimSuffix(slug, "-"+id), "-", " ")
			if title = strings.TrimSpace(title); title != "" {
				return title
			}
		}
	}
	return "Pexels video " + id
}
