package octopus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumption(t *testing.T) {
	var srv *httptest.Server

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, _, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "sk_test", key)
		assert.Equal(t, "/v1/gas-meter-points/123/meters/G4/consumption/", r.URL.Path)

		if r.URL.Query().Get("page") == "" {
			fmt.Fprintf(w, `{"next":%q,"results":[{"consumption":1,"interval_start":"2026-10-24T00:00:00+01:00"}]}`,
				srv.URL+r.URL.Path+"?page=2")

			return
		}

		fmt.Fprint(w, `{"next":null,"results":[{"consumption":0,"interval_start":"2026-10-24T00:30:00+01:00"}]}`)
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Key: "sk_test", Fuel: Gas, Point: "123", Serial: "G4", Units: M3, CalorificValue: 39.5}

	got, err := c.Consumption(t.Context(), time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, time.Date(2026, 10, 23, 23, 0, 0, 0, time.UTC), got[0].Start)
	assert.InDelta(t, 1*1.02264*39.5/3.6, got[0].KWh, 1e-9)
	assert.Zero(t, got[1].KWh)
}

func TestConsumptionKWh(t *testing.T) {
	c := Client{Fuel: Gas, Units: KWh, CalorificValue: 39.5}
	assert.Equal(t, 0.75, c.kWh(0.75))
}

func TestConsumptionElectricity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/electricity-meter-points/456/meters/E1/consumption/", r.URL.Path)
		fmt.Fprint(w, `{"next":null,"results":[{"consumption":0.4,"interval_start":"2026-10-24T00:00:00Z"}]}`)
	}))
	defer srv.Close()

	// Electricity is energy whatever the units say
	c := Client{BaseURL: srv.URL, Key: "sk_test", Fuel: Electricity, Point: "456", Serial: "E1", Units: M3, CalorificValue: 39.5}

	got, err := c.Consumption(t.Context(), time.Now())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 0.4, got[0].KWh)
}

func TestConsumptionForeignNextPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"next":"https://example.com/steal","results":[]}`)
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Key: "sk_test", Fuel: Gas, Point: "123", Serial: "G4", Units: KWh}

	_, err := c.Consumption(t.Context(), time.Now())
	assert.ErrorContains(t, err, "unexpected next page")
}

func TestConsumptionStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Key: "bad", Fuel: Gas, Point: "123", Serial: "G4", Units: KWh}

	_, err := c.Consumption(t.Context(), time.Now())
	assert.ErrorContains(t, err, "401")
}
