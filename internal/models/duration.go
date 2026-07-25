package models

import (
	"fmt"
	"time"
)

type DisableDuration string

const (
	DurationIndefinite DisableDuration = ""
	Duration10Min      DisableDuration = "10m"
	Duration1Hour      DisableDuration = "1h"
	Duration24Hours    DisableDuration = "24h"
)

var AllowedDurations = map[DisableDuration]time.Duration{
	Duration10Min:   10 * time.Minute,
	Duration1Hour:   1 * time.Hour,
	Duration24Hours: 24 * time.Hour,
}

func ParseDuration(d string) (time.Duration, error) {
	if d == "" {
		return 0, nil
	}
	dur, ok := AllowedDurations[DisableDuration(d)]
	if !ok {
		return 0, fmt.Errorf("invalid duration %q: allowed values are 10m, 1h, 24h, or empty for indefinite", d)
	}
	return dur, nil
}

func ComputeDisabledUntil(d DisableDuration) *time.Time {
	if d == DurationIndefinite {
		return nil
	}
	dur, err := ParseDuration(string(d))
	if err != nil {
		return nil
	}
	t := time.Now().Add(dur)
	return &t
}
