package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
)

const ImageSourceTypeHttp ImageSourceType = "http"

// Currently only passes headers required for cache control, not validation
// As per https://developer.mozilla.org/en-US/docs/Web/HTTP/Caching
var cacheHeaders = [...]string{
	"Cache-Control",
	"Expires",
	"Last-Modified",
	"Pragma",
	"Vary",
}

func isCacheHeader(headerName string) bool {
	for _, v := range cacheHeaders {
		if headerName == v {
			return true
		}
	}
	return false
}

type HttpImageSource struct {
	Config *SourceConfig
}

func NewHttpImageSource(config *SourceConfig) ImageSource {
	return &HttpImageSource{config}
}

func (s *HttpImageSource) Matches(r *http.Request) bool {
	return r.Method == "GET" && r.URL.Query().Get("url") != ""
}

func (s *HttpImageSource) GetImage(req *http.Request) ([]byte, error) {
	buf, _, err := s.GetImageWithCacheHeaders(req)

	return buf, err
}

func (s *HttpImageSource) GetImageWithCacheHeaders(req *http.Request) ([]byte, http.Header, error) {
	url, err := parseURL(req)
	if err != nil {
		return nil, nil, ErrInvalidImageURL
	}
	if shouldRestrictOrigin(url, s.Config.AllowedOrigings) {
		return nil, nil, fmt.Errorf("Not allowed remote URL origin: %s", url.Host)
	}
	return s.fetchImage(url, req)
}

func (s *HttpImageSource) fetchImage(url *url.URL, ireq *http.Request) ([]byte, http.Header, error) {
	// Check remote image size by fetching HTTP Headers
	if s.Config.MaxAllowedSize > 0 {
		req := newHTTPRequest(s, ireq, "HEAD", url)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("Error fetching image http headers: %v", err)
		}
		res.Body.Close()
		if res.StatusCode < 200 && res.StatusCode > 206 {
			return nil, nil, fmt.Errorf("Error fetching image http headers: (status=%d) (url=%s)", res.StatusCode, req.URL.String())
		}

		contentLength, _ := strconv.Atoi(res.Header.Get("Content-Length"))
		if contentLength > s.Config.MaxAllowedSize {
			return nil, nil, fmt.Errorf("Content-Length %d exceeds maximum allowed %d bytes", contentLength, s.Config.MaxAllowedSize)
		}
	}

	// Perform the request using the default client
	req := newHTTPRequest(s, ireq, "GET", url)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("Error downloading image: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, nil, fmt.Errorf("Error downloading image: (status=%d) (url=%s)", res.StatusCode, req.URL.String())
	}

	// Gather the cache headers
	resHeaders := make(http.Header, len(res.Header))
	for k, v := range res.Header {
		if isCacheHeader(k) {
			for _, vv := range v {
				resHeaders.Add(k, vv)
			}
		}
	}

	// Read the body
	buf, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("Unable to create image from response body: %s (url=%s)", req.URL.String(), err)
	}
	return buf, resHeaders, nil
}

func (s *HttpImageSource) setAuthorizationHeader(req *http.Request, ireq *http.Request) {
	auth := s.Config.Authorization
	if auth == "" {
		auth = ireq.Header.Get("X-Forward-Authorization")
	}
	if auth == "" {
		auth = ireq.Header.Get("Authorization")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
}

func parseURL(request *http.Request) (*url.URL, error) {
	queryUrl := request.URL.Query().Get("url")
	return url.Parse(queryUrl)
}

func newHTTPRequest(s *HttpImageSource, ireq *http.Request, method string, url *url.URL) *http.Request {
	req, _ := http.NewRequest(method, url.String(), nil)
	req.Header.Set("User-Agent", "imaginary/"+Version)
	req.URL = url

	// Forward auth header to the target server, if necessary
	if s.Config.AuthForwarding || s.Config.Authorization != "" {
		s.setAuthorizationHeader(req, ireq)
	}

	return req
}

func shouldRestrictOrigin(url *url.URL, origins []*url.URL) bool {
	if len(origins) == 0 {
		return false
	}
	for _, origin := range origins {
		if origin.Host == url.Host {
			return false
		}
	}
	return true
}

func init() {
	RegisterSource(ImageSourceTypeHttp, NewHttpImageSource)
}
