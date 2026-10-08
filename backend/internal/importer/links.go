package importer

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// LinkWarning reports an unconfirmed source, never an import failure.
// A forbidden response can mean bot protection rather than a broken page.
type LinkWarning struct {
	PlaceSlug string
	URL       string
	Reason    string
}

// CheckLinks checks each distinct URL once with at most four concurrent requests.
// Results follow the input order, including references shared by multiple places.
func CheckLinks(ctx context.Context, places []Place, client *http.Client, timeout time.Duration) []LinkWarning {
	urls := []string{}
	indices := map[string]int{}
	for _, place := range places {
		for _, link := range place.Links {
			if _, exists := indices[link.URL]; !exists {
				indices[link.URL] = len(urls)
				urls = append(urls, link.URL)
			}
		}
	}
	reasons := make([]string, len(urls))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				checkCtx, cancel := context.WithTimeout(ctx, timeout)
				reasons[index] = checkURL(checkCtx, client, urls[index])
				cancel()
			}
		}()
	}
	for index := range urls {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	warnings := []LinkWarning{}
	for _, place := range places {
		for _, link := range place.Links {
			if reason := reasons[indices[link.URL]]; reason != "" {
				warnings = append(warnings, LinkWarning{PlaceSlug: place.Slug, URL: link.URL, Reason: reason})
			}
		}
	}
	return warnings
}

func checkURL(ctx context.Context, client *http.Client, address string) string {
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		request, err := http.NewRequestWithContext(ctx, method, address, nil)
		if err != nil {
			return fmt.Sprintf("invalid request: %v", err)
		}
		request.Header.Set("User-Agent", "Vandrouka-LinkChecker/0.1")
		response, err := client.Do(request)
		if err != nil {
			return fmt.Sprintf("availability not confirmed: %v", err)
		}
		response.Body.Close()
		if method == http.MethodHead && (response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented) {
			continue
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return ""
		}
		return fmt.Sprintf("availability not confirmed: HTTP %d", response.StatusCode)
	}
	return "availability not confirmed"
}
