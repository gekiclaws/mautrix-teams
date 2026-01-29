package model

import (
	"strconv"
	"strings"
)

type ConsumptionHorizonsResponse struct {
	ID       string               `json:"id"`
	Version  string               `json:"version"`
	Horizons []ConsumptionHorizon `json:"consumptionhorizons"`
}

type ConsumptionHorizon struct {
	ID                    string `json:"id"`
	ConsumptionHorizon    string `json:"consumptionhorizon"`
	MessageVisibilityTime int64  `json:"messageVisibilityTime"`
}

func ParseConsumptionHorizonLatestReadTS(horizon string) (int64, bool) {
	parts := strings.Split(horizon, ";")
	if len(parts) < 2 {
		return 0, false
	}
	value := strings.TrimSpace(parts[1])
	if value == "" {
		return 0, false
	}
	ts, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return ts, true
}
