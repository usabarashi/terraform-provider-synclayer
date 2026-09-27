package synclayer

import (
	"context"
	"net/http"
)

// ---------------------------------------------------------------------------
// Eco mode (/gateway/eco)
// ---------------------------------------------------------------------------

// EcoMode mirrors the eco block of GET /api/v1/gateway/eco.
//
// The device reports option metadata (month and day names) alongside it and
// answers a write with 202; verified on VER-01.06.05-EA that a body carrying
// only this block is accepted and applied.
type EcoMode struct {
	Active          bool   `json:"enable"`
	Type            int    `json:"type"`
	ScheduleEnabled bool   `json:"scheduleEnable"`
	StartTime       string `json:"startTime"`
	EndTime         string `json:"endTime"`
}

type ecoModeResponse struct {
	Eco EcoMode `json:"eco"`
}

// GetEcoMode returns the eco mode configuration.
func (c *Client) GetEcoMode(ctx context.Context) (*EcoMode, error) {
	var resp ecoModeResponse
	if err := c.request(ctx, http.MethodGet, "/gateway/eco", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Eco, nil
}

// UpdateEcoMode writes the eco mode configuration.
func (c *Client) UpdateEcoMode(ctx context.Context, mode EcoMode) error {
	return c.request(ctx, http.MethodPut, "/gateway/eco", map[string]any{"eco": mode}, nil)
}

// ---------------------------------------------------------------------------
// Date and time (/gateway/datetime)
// ---------------------------------------------------------------------------

// DateTimeDST is the daylight saving block of the date and time settings.
type DateTimeDST struct {
	Active    bool   `json:"active"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

// DateTimeNTP is the NTP block of the date and time settings.
type DateTimeNTP struct {
	Active  bool     `json:"active"`
	Servers []string `json:"server"`
}

// DateTime mirrors the writable part of GET /api/v1/gateway/datetime. The
// device also reports the current time and the list of time zones; neither is
// sent back.
type DateTime struct {
	DaylightSaving DateTimeDST `json:"daylightSavingTime"`
	NTP            DateTimeNTP `json:"ntp"`
	TimeZone       int         `json:"timeZone"`
	TimeZoneName   string      `json:"timeZoneName,omitempty"`
}

// GetDateTime returns the date and time settings.
func (c *Client) GetDateTime(ctx context.Context) (*DateTime, error) {
	var dt DateTime
	if err := c.request(ctx, http.MethodGet, "/gateway/datetime", nil, &dt); err != nil {
		return nil, err
	}
	return &dt, nil
}

// UpdateDateTime writes the date and time settings.
//
// Verified on VER-01.06.05-EA that leaving out the current time and the time
// zone list is accepted and changes nothing about the rest.
func (c *Client) UpdateDateTime(ctx context.Context, dt DateTime) error {
	return c.request(ctx, http.MethodPut, "/gateway/datetime", dt, nil)
}

// RefreshDateTime asks the device to take its time from the NTP servers now.
func (c *Client) RefreshDateTime(ctx context.Context) error {
	return c.request(ctx, http.MethodPost, "/gateway/datetime/refresh", map[string]any{}, nil)
}
