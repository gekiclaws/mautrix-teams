package auth

// RegionGTMs holds the region-specific service endpoints returned by the Enterprise authz endpoint.
type RegionGTMs struct {
	ChatService           string `json:"chatService"`
	ChatServiceAggregator string `json:"chatServiceAggregator"`
	MiddleTier            string `json:"middleTier"`
	UnifiedPresence       string `json:"unifiedPresence"`
	AMS                   string `json:"ams"`
	Raw                   map[string]interface{} `json:"-"`
}

// ParseRegionGTMs extracts known fields from the raw regionGtms map.
func ParseRegionGTMs(raw map[string]interface{}) *RegionGTMs {
	if raw == nil {
		return nil
	}
	gtms := &RegionGTMs{Raw: raw}
	if v, ok := raw["chatService"].(string); ok {
		gtms.ChatService = v
	}
	if v, ok := raw["chatServiceAggregator"].(string); ok {
		gtms.ChatServiceAggregator = v
	}
	if v, ok := raw["middleTier"].(string); ok {
		gtms.MiddleTier = v
	}
	if v, ok := raw["unifiedPresence"].(string); ok {
		gtms.UnifiedPresence = v
	}
	if v, ok := raw["ams"].(string); ok {
		gtms.AMS = v
	}
	return gtms
}
