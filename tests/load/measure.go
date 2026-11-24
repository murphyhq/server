//go:build load

package load

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"sync"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/client"
)

func pageDelays(t *testing.T, murphy *client.ClientWithResponses, serviceID string) (delays []time.Duration, unpaged int) {
	t.Helper()
	ids := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range ids {
				got, err := murphy.GetIncidentWithResponse(t.Context(), id)
				if err != nil || got.StatusCode() != http.StatusOK {
					mu.Lock()
					unpaged++
					mu.Unlock()
					continue
				}
				delay, paged := firstPage(got.JSON200)
				mu.Lock()
				if paged {
					delays = append(delays, delay)
				} else {
					unpaged++
				}
				mu.Unlock()
			}
		}()
	}
	limit := 100
	params := &client.ListIncidentsParams{ServiceId: &[]string{serviceID}, Limit: &limit}
	for {
		page, err := murphy.ListIncidentsWithResponse(t.Context(), params)
		if err != nil || page.StatusCode() != http.StatusOK {
			break
		}
		for _, incident := range page.JSON200.Items {
			ids <- incident.Id
		}
		if page.JSON200.NextCursor == "" {
			break
		}
		params.Cursor = &page.JSON200.NextCursor
	}
	close(ids)
	wg.Wait()
	return delays, unpaged
}

func firstPage(incident *client.IncidentWithTimeline) (time.Duration, bool) {
	for _, entry := range incident.Timeline {
		if entry.Type == client.TimelineEntryTypeNotified && entry.Notification.Reason == client.TimelineNotificationReasonPaged {
			return entry.At.Sub(incident.CreatedAt), true
		}
	}
	return 0, false
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(float64(len(sorted))*p))]
}

func incidents(murphy *client.ClientWithResponses, serviceID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	limit := 100
	params := &client.ListIncidentsParams{ServiceId: &[]string{serviceID}, Limit: &limit}
	count := 0
	for {
		page, err := murphy.ListIncidentsWithResponse(ctx, params)
		if err != nil {
			return count, fmt.Errorf("list incidents: %w", err)
		}
		if page.StatusCode() != http.StatusOK {
			return count, fmt.Errorf("list incidents: %s", page.Status())
		}
		count += len(page.JSON200.Items)
		if page.JSON200.NextCursor == "" {
			return count, nil
		}
		params.Cursor = &page.JSON200.NextCursor
	}
}

type acceptance struct {
	mu     sync.Mutex
	byPath map[string]int
}

func acceptedByPath() *acceptance {
	return &acceptance{byPath: map[string]int{}}
}

func (a *acceptance) observe(result *vegeta.Result) {
	if result.Code != http.StatusAccepted {
		return
	}
	parsed, err := neturl.Parse(result.URL)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.byPath[parsed.Path]++
}

func (a *acceptance) count(path string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.byPath[path]
}
