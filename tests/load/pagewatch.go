//go:build load

package load

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
)

type pageWatch struct {
	murphy    *client.ClientWithResponses
	serviceID string
	listed    bool
	pending   []string
	delays    []time.Duration
}

func newPageWatch(murphy *client.ClientWithResponses, serviceID string) *pageWatch {
	return &pageWatch{murphy: murphy, serviceID: serviceID}
}

func (w *pageWatch) awaitAll(t *testing.T, settle time.Duration) {
	t.Helper()
	deadline := time.Now().Add(settle)
	for {
		started := time.Now()
		w.scan(t)
		if len(w.pending) == 0 {
			return
		}
		require.True(t, time.Now().Before(deadline), "every incident's first level gets its webhook: %d still waiting", len(w.pending))
		time.Sleep(max(0, time.Second-time.Since(started)))
	}
}

func (w *pageWatch) scan(t *testing.T) {
	t.Helper()
	if !w.listed {
		w.pending = w.listIncidents(t)
		w.listed = true
	}
	var mu sync.Mutex
	var waiting []string
	ids := make(chan string)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range ids {
				delay, paged := w.fetchFirstPage(t, id)
				mu.Lock()
				if paged {
					w.delays = append(w.delays, delay)
				} else {
					waiting = append(waiting, id)
				}
				mu.Unlock()
			}
		}()
	}
	for _, id := range w.pending {
		ids <- id
	}
	close(ids)
	wg.Wait()
	w.pending = waiting
}

func (w *pageWatch) listIncidents(t *testing.T) []string {
	t.Helper()
	var ids []string
	limit := 100
	params := &client.ListIncidentsParams{ServiceId: &[]string{w.serviceID}, Limit: &limit}
	for {
		page, err := w.murphy.ListIncidentsWithResponse(t.Context(), params)
		require.NoError(t, err, "list incidents")
		require.Equal(t, http.StatusOK, page.StatusCode(), "list incidents: %s", page.Body)
		for _, incident := range page.JSON200.Items {
			ids = append(ids, incident.Id)
		}
		if page.JSON200.NextCursor == "" {
			return ids
		}
		params.Cursor = &page.JSON200.NextCursor
	}
}

func (w *pageWatch) fetchFirstPage(t *testing.T, id string) (time.Duration, bool) {
	t.Helper()
	got, err := w.murphy.GetIncidentWithResponse(t.Context(), id)
	if err != nil || got.StatusCode() != http.StatusOK {
		return 0, false
	}
	return firstPage(got.JSON200)
}
