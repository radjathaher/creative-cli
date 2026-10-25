package broll

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

type pixabayResponse struct {
	Hits []pixabayVideo `json:"hits"`
}

type pixabayVideo struct {
	ID       int64                       `json:"id"`
	PageURL  string                      `json:"pageURL"`
	Tags     string                      `json:"tags"`
	Duration float64                     `json:"duration"`
	User     string                      `json:"user"`
	UserID   int64                       `json:"user_id"`
	Videos   map[string]pixabayVideoFile `json:"videos"`
}

type pixabayVideoFile struct {
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Thumbnail string `json:"thumbnail"`
}

func (c *Client) searchPixabay(ctx context.Context, o SearchOptions) ([]Result, error) {
	if utf8.RuneCountInString(o.Query) > 100 {
		return nil, fmt.Errorf("pixabay: query must not exceed 100 characters")
	}
	limit := o.Limit
	if limit <= 0 {
		limit = 20
	}
	params := url.Values{"q": {o.Query}, "per_page": {strconv.Itoa(min(200, max(3, limit)))}}
	response, err := c.requestPixabay(ctx, params)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(response.Hits))
	for _, hit := range response.Hits {
		if result, ok := hit.result(o.Orientation, o.Quality); ok {
			result.Query = o.Query
			results = append(results, result)
		}
	}
	return results, nil
}

func (c *Client) getPixabay(ctx context.Context, id, quality string) (Result, error) {
	numericID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || numericID <= 0 {
		return Result{}, fmt.Errorf("pixabay: video ID must be a positive integer")
	}
	response, err := c.requestPixabay(ctx, url.Values{"id": {strconv.FormatInt(numericID, 10)}})
	if err != nil {
		return Result{}, err
	}
	for _, hit := range response.Hits {
		if hit.ID != numericID {
			continue
		}
		if result, ok := hit.result("", quality); ok {
			return result, nil
		}
		return Result{}, fmt.Errorf("pixabay: video has no matching download rendition")
	}
	return Result{}, fmt.Errorf("pixabay: requested video was not found")
}

func (c *Client) requestPixabay(ctx context.Context, params url.Values) (pixabayResponse, error) {
	var response pixabayResponse
	key, err := c.ReadSecret("PIXABAY_API_KEY")
	if err != nil || strings.TrimSpace(key) == "" {
		return response, fmt.Errorf("pixabay: PIXABAY_API_KEY is not configured")
	}
	endpoint := c.PixabayURL
	if endpoint == "" {
		endpoint = "https://pixabay.com/api/videos/"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return response, fmt.Errorf("pixabay: invalid API endpoint")
	}
	query := u.Query()
	for name, values := range params {
		query[name] = values
	}
	query.Set("key", strings.TrimSpace(key))
	u.RawQuery = query.Encode()
	if err := c.getJSON(ctx, "pixabay", u.String(), nil, &response); err != nil {
		return response, err
	}
	if response.Hits == nil {
		return response, fmt.Errorf("pixabay: response is missing the video results array")
	}
	return response, nil
}

func (v pixabayVideo) result(wantOrientation, quality string) (Result, bool) {
	if v.ID <= 0 || v.Duration <= 0 || !validURL(v.PageURL) {
		return Result{}, false
	}
	files := make([]videoFile, 0, len(v.Videos))
	for _, size := range []string{"large", "medium", "small", "tiny"} {
		file := v.Videos[size]
		files = append(files, videoFile{
			URL: file.URL, MIME: "video/mp4", Width: file.Width, Height: file.Height,
			Thumbnail: file.Thumbnail,
		})
	}
	selected, ok := selectVideo(files, wantOrientation, quality)
	if !ok {
		return Result{}, false
	}
	authorURL := ""
	if v.User != "" && v.UserID > 0 {
		authorURL = "https://pixabay.com/users/" + url.PathEscape(v.User) + "-" + strconv.FormatInt(v.UserID, 10) + "/"
	}
	return Result{
		ID: "pixabay:" + strconv.FormatInt(v.ID, 10), Provider: "pixabay", Title: v.Tags,
		DurationS: v.Duration, Width: selected.Width, Height: selected.Height,
		Orientation: orientation(selected.Width, selected.Height), PreviewURL: selected.URL,
		ThumbnailURL: selected.Thumbnail, PageURL: v.PageURL, Author: v.User, AuthorURL: authorURL,
		Licence: "Pixabay Content License", LicenceURL: "https://pixabay.com/service/license-summary/",
		DownloadURL: selected.URL, CostUSD: 0,
	}, true
}
