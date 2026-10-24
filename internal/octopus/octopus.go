// Package octopus reads gas and electricity consumption from the Octopus Energy API.
package octopus

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// BaseURL is the Octopus Energy API.
	BaseURL = "https://api.octopus.energy"

	// pageSize is the most results the API returns in a page.
	pageSize = 25000

	// maxPages bounds the pages followed in one call.
	maxPages = 10

	// correction is the volume correction factor, and mjPerKWh converts MJ to kWh; together with the
	// calorific value they turn a volume of gas into energy.
	correction = 1.02264
	mjPerKWh   = 3.6
)

// Fuel is what a meter measures, as named in the API.
type Fuel string

const (
	// FuelGas is a gas meter.
	FuelGas Fuel = "gas"

	// FuelElectricity is an electricity meter.
	FuelElectricity Fuel = "electricity"
)

// Units is what the meter reports consumption in.
type Units string

const (
	// UnitsM3 is volume in cubic metres, which SMETS2 gas meters report.
	UnitsM3 Units = "m3"

	// UnitsKWh is energy in kilowatt hours, which SMETS1 gas meters and all electricity meters report.
	UnitsKWh Units = "kwh"
)

// Interval is the energy used in a half hour.
type Interval struct {
	// Start is when the half hour began.
	Start time.Time

	// KWh is the energy used.
	KWh float64
}

// Client reads the consumption of a meter.
type Client struct {
	// BaseURL is the API; BaseURL unless testing.
	BaseURL string

	// Key is the API key.
	Key string

	// Fuel is what the meter measures.
	Fuel Fuel

	// Point is the MPRN of a gas meter or the MPAN of an electricity meter, and Serial is the meter's.
	Point, Serial string

	// Units is what a gas meter reports consumption in.
	Units Units

	// CalorificValue is the MJ/m³ of the gas, from the bill, to convert a volume to energy.
	CalorificValue float64

	// HTTP is the client to use; http.DefaultClient if nil.
	HTTP *http.Client
}

// reading is one half hour of consumption in the response.
type reading struct {
	Consumption float64   `json:"consumption"`
	Start       time.Time `json:"interval_start"`
}

// page is a page of the response.
type page struct {
	Next    string    `json:"next"`
	Results []reading `json:"results"`
}

// Consumption returns the half hours which started at or after from, oldest first.
func (c *Client) Consumption(ctx context.Context, from time.Time) ([]Interval, error) {
	q := url.Values{
		"period_from": {from.UTC().Format(time.RFC3339)},
		"order_by":    {"period"},
		"page_size":   {fmt.Sprint(pageSize)},
	}

	next := fmt.Sprintf(
		"%s/v1/%s-meter-points/%s/meters/%s/consumption/?%s",
		c.BaseURL,
		c.Fuel,
		url.PathEscape(c.Point),
		url.PathEscape(c.Serial),
		q.Encode(),
	)

	var intervals []Interval

	for range maxPages {
		p, err := c.get(ctx, next)
		if err != nil {
			return nil, err
		}

		for _, r := range p.Results {
			i := Interval{Start: r.Start.UTC(), KWh: c.kWh(r.Consumption)}
			intervals = append(intervals, i)
		}

		if p.Next == "" {
			return intervals, nil
		}

		// The key is sent with every request, so only follow links on the API
		if !strings.HasPrefix(p.Next, c.BaseURL+"/") {
			return nil, fmt.Errorf("unexpected next page %q", p.Next)
		}

		next = p.Next
	}

	return nil, fmt.Errorf("more than %d pages of consumption", maxPages)
}

// kWh returns the energy in a consumption reading.
func (c *Client) kWh(v float64) float64 {
	if c.Fuel == FuelElectricity || c.Units == UnitsKWh {
		return v
	}

	return v * correction * c.CalorificValue / mjPerKWh
}

// get returns a page of consumption.
func (c *Client) get(ctx context.Context, u string) (page, error) {
	var p page

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return p, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.Key, "")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return p, fmt.Errorf("failed to get consumption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return p, fmt.Errorf("failed to get consumption: %s", resp.Status)
	}

	err = json.UnmarshalRead(resp.Body, &p)
	if err != nil {
		return p, fmt.Errorf("failed to read consumption: %w", err)
	}

	return p, nil
}
