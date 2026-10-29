package domain

import "math"

const EarthRadiusKM = 6371.0088

// Haversine returns the great-circle distance between two points in kilometers.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	lat1r := lat1 * math.Pi / 180
	lng1r := lng1 * math.Pi / 180
	lat2r := lat2 * math.Pi / 180
	lng2r := lng2 * math.Pi / 180
	dLat := lat2r - lat1r
	dLng := lng2r - lng1r
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1r)*math.Cos(lat2r)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * EarthRadiusKM * math.Asin(math.Min(1, math.Sqrt(a)))
}

// RadiusTiers implements the "first nearby, then further away" search.
// It preserves the requested radius, then progressively expands, capped at the
// globe. Duplicate tiers are removed.
func RadiusTiers(radiusKM float64) []float64 {
	if radiusKM <= 0 {
		radiusKM = 10
	}
	tiers := []float64{radiusKM, radiusKM * 2.5, radiusKM * 6, 100, 250, 1000, 5000}
	out := make([]float64, 0, len(tiers))
	var last float64
	for _, t := range tiers {
		t = math.Round(t*10) / 10
		if t <= last {
			continue
		}
		out = append(out, t)
		last = t
	}
	return out
}

func Clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
