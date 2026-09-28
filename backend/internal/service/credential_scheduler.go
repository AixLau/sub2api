package service

import "math"

// CredentialDemand contains only soft scheduling inputs. Targets never approve
// work or revoke already occupied capacity. Admission rechecks every hard limit.
type CredentialDemand struct {
	ID                           int64
	Weight, Demand, HardCapacity float64
}

func CredentialTargets(total float64, in []CredentialDemand) map[int64]float64 {
	result := make(map[int64]float64, len(in))
	active := make([]CredentialDemand, 0, len(in))
	remaining := math.Max(0, total)
	for _, d := range in {
		result[d.ID] = 0
		if d.Weight > 0 && !math.IsNaN(d.Weight) && !math.IsInf(d.Weight, 0) && d.Demand > 0 && d.HardCapacity > 0 {
			active = append(active, d)
		}
	}
	for len(active) > 0 && remaining > 0 {
		weight := 0.0
		for _, d := range active {
			weight += d.Weight
		}
		fixed := false
		next := make([]CredentialDemand, 0, len(active))
		level := remaining / weight
		for _, d := range active {
			cap := math.Min(d.Demand, d.HardCapacity)
			if cap <= level*d.Weight {
				result[d.ID] = cap
				remaining -= cap
				fixed = true
			} else {
				next = append(next, d)
			}
		}
		if !fixed {
			for _, d := range active {
				result[d.ID] = level * d.Weight
			}
			break
		}
		active = next
	}
	return result
}
