package modelconfig

import (
	"strconv"
	"strings"
)

// ResolutionSwitches projects unset switches from configured prices without
// writing defaults. An explicit map is authoritative, including all-off.
func (m ConfiguredModel) ResolutionSwitches(p *MediaProfile) map[string]bool {
	if p == nil {
		return nil
	}
	out := map[string]bool{}
	for _, r := range p.Resolutions {
		if m.ResolutionEnabled != nil {
			out[r.ID] = m.ResolutionEnabled[r.ID]
			continue
		}
		out[r.ID] = false
		if m.CreditPrice != nil {
			_, out[r.ID] = m.CreditPrice.AtResolution(r.ID, p.DefaultResolution)
		}
	}
	return out
}

// EnabledMediaProfile is the user capability view. The operator profile keeps
// the supplier default unchanged because it is also the unitPrice basis.
func (m ConfiguredModel) EnabledMediaProfile(p *MediaProfile) *MediaProfile {
	if p == nil {
		return nil
	}
	out := *p
	out.Resolutions = []MediaResolution{}
	switches := m.ResolutionSwitches(p)
	for _, r := range p.Resolutions {
		if switches[r.ID] {
			out.Resolutions = append(out.Resolutions, r)
		}
	}
	out.DefaultResolution = ""
	for _, r := range out.Resolutions {
		if out.DefaultResolution == "" || resolutionSize(r.ID) > resolutionSize(out.DefaultResolution) {
			out.DefaultResolution = r.ID
		}
	}
	if m.Category != "image" && out.HasResolution(p.DefaultResolution) {
		out.DefaultResolution = p.DefaultResolution
	}
	return &out
}

func resolutionSize(id string) float64 {
	id = strings.ToLower(id)
	if parts := strings.Split(id, "x"); len(parts) == 2 {
		w, _ := strconv.ParseFloat(parts[0], 64)
		h, _ := strconv.ParseFloat(parts[1], 64)
		return w * h
	}
	multiplier := 1.0
	if strings.HasSuffix(id, "k") {
		multiplier = 1024
	}
	n, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(id, "k"), "p"), 64)
	return n * multiplier
}
