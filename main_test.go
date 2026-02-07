package main

import (
	"fmt"
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Tier 1: Pure utility functions
// ---------------------------------------------------------------------------

func TestWmoDescription(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "Clear"},
		{1, "Mostly Clear"},
		{3, "Overcast"},
		{45, "Fog"},
		{61, "Lt Rain"},
		{75, "Hvy Snow"},
		{95, "Tstorm"},
		{99, "Tstorm+Hvy Hail"},
		{-1, "Unknown"},
		{100, "Unknown"},
		{999, "Unknown"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("code_%d", tt.code), func(t *testing.T) {
			got := wmoDescription(tt.code)
			if got != tt.want {
				t.Errorf("wmoDescription(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestWindDirLabel(t *testing.T) {
	tests := []struct {
		degrees float64
		want    string
	}{
		{0, "N"},
		{45, "NE"},
		{90, "E"},
		{135, "SE"},
		{180, "S"},
		{225, "SW"},
		{270, "W"},
		{315, "NW"},
		{360, "N"},     // wrap-around
		{11.25, "NNE"}, // boundary: 11.25/22.5=0.5, Round rounds away from zero
		{11.26, "NNE"}, // just past boundary
		{348.75, "N"},  // just before wrap
		{22.5, "NNE"},  // exact boundary
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.2f", tt.degrees), func(t *testing.T) {
			got := windDirLabel(tt.degrees)
			if got != tt.want {
				t.Errorf("windDirLabel(%.2f) = %q, want %q", tt.degrees, got, tt.want)
			}
		})
	}
}

func TestTempToColor(t *testing.T) {
	tests := []struct {
		name string
		temp float64
		want string
	}{
		// Clamped extremes
		{"very cold clamped", -10, "#4a9eff"},
		{"exactly first stop", 20, "#4a9eff"},
		{"exactly last stop", 95, "#ff3333"},
		{"very hot clamped", 120, "#ff3333"},
		// Mid-point between stops
		{"midpoint 20-45", 32.5, "#95bfef"}, // interpolation between blue and white
		{"midpoint 65-80", 72.5, "#ff a234"}, // between yellow and orange — computed below
	}
	// Correct the midpoint between yellow(ffd93d) and orange(ff6b35) at t=0.5:
	// r = 0xff, g = 0xd9 + 0.5*(0x6b-0xd9) = 217 + 0.5*(-110) = 162 = 0xa2, b = 0x3d + 0.5*(0x35-0x3d) = 61+(-4) = 57 = 0x39
	tests[5].want = "#ffa239"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tempToColor(tt.temp)
			if got != tt.want {
				t.Errorf("tempToColor(%.1f) = %q, want %q", tt.temp, got, tt.want)
			}
		})
	}

	// Verify format is always #rrggbb
	for temp := -20.0; temp <= 120.0; temp += 5 {
		c := tempToColor(temp)
		if len(c) != 7 || c[0] != '#' {
			t.Errorf("tempToColor(%.0f) = %q, expected #rrggbb format", temp, c)
		}
	}
}

func TestWindchill(t *testing.T) {
	tests := []struct {
		name    string
		temp    float64
		wind    float64
		want    int
	}{
		// Conditions where formula does NOT apply
		{"warm temp", 50, 10, 50},
		{"above 50", 60, 20, 60},
		{"low wind", 30, 3, 30},
		{"low wind exact", 30, 2, 30},
		// Conditions where formula applies
		{"moderate cold/wind", 30, 10, 21},
		{"cold and windy", 0, 15, -19},
		{"freezing with wind", 20, 20, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := windchill(tt.temp, tt.wind)
			if got != tt.want {
				t.Errorf("windchill(%.0f, %.0f) = %d, want %d", tt.temp, tt.wind, got, tt.want)
			}
		})
	}
}

func TestParseWindSpeed(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"10 mph", 10},
		{"5 to 10 mph", 5}, // parses first number
		{"0 mph", 0},
		{"25 mph", 25},
		{"", 0}, // no number found
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseWindSpeed(tt.input)
			if got != tt.want {
				t.Errorf("parseWindSpeed(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

func TestHaversine(t *testing.T) {
	tests := []struct {
		name                       string
		lat1, lon1, lat2, lon2     float64
		wantMin, wantMax           float64 // acceptable range in km
	}{
		{"same point", 40.7128, -74.0060, 40.7128, -74.0060, 0, 0.001},
		// NYC to LA: ~3944 km
		{"NYC to LA", 40.7128, -74.0060, 34.0522, -118.2437, 3930, 3960},
		// London to Paris: ~344 km
		{"London to Paris", 51.5074, -0.1278, 48.8566, 2.3522, 340, 350},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := haversine(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("haversine() = %.2f km, want [%.2f, %.2f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestParseISO8601Duration(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"PT1H", 1},
		{"PT6H", 6},
		{"PT12H", 12},
		{"PT24H", 24},
		{"INVALID", 6},   // default fallback
		{"PT30M", 6},     // no hour component → fallback
		{"", 6},           // empty → fallback
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseISO8601Duration(tt.input)
			if got != tt.want {
				t.Errorf("parseISO8601Duration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetDailyRowForDate(t *testing.T) {
	days := []DailyRow{
		{DateISO: "2026-02-05", High: 45},
		{DateISO: "2026-02-06", High: 50},
		{DateISO: "2026-02-07", High: 55},
	}

	t.Run("found", func(t *testing.T) {
		got := getDailyRowForDate(days, "2026-02-06")
		if got == nil || got.High != 50 {
			t.Errorf("expected row with High=50, got %v", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := getDailyRowForDate(days, "2026-02-10")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		got := getDailyRowForDate(nil, "2026-02-06")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestGetAdjacentDates(t *testing.T) {
	days := []DailyRow{
		{DateISO: "2026-02-05"},
		{DateISO: "2026-02-06"},
		{DateISO: "2026-02-07"},
	}

	t.Run("middle element", func(t *testing.T) {
		prev, next := getAdjacentDates(days, "2026-02-06")
		if prev != "2026-02-05" || next != "2026-02-07" {
			t.Errorf("got prev=%q, next=%q", prev, next)
		}
	})

	t.Run("first element", func(t *testing.T) {
		prev, next := getAdjacentDates(days, "2026-02-05")
		if prev != "" || next != "2026-02-06" {
			t.Errorf("got prev=%q, next=%q", prev, next)
		}
	})

	t.Run("last element", func(t *testing.T) {
		prev, next := getAdjacentDates(days, "2026-02-07")
		if prev != "2026-02-06" || next != "" {
			t.Errorf("got prev=%q, next=%q", prev, next)
		}
	})

	t.Run("not found", func(t *testing.T) {
		prev, next := getAdjacentDates(days, "2026-02-10")
		if prev != "" || next != "" {
			t.Errorf("got prev=%q, next=%q", prev, next)
		}
	})
}

func TestGeocodeToSearchResults(t *testing.T) {
	t.Run("with admin1", func(t *testing.T) {
		input := []GeocodeResult{
			{Name: "Portland", Latitude: 45.5, Longitude: -122.6, Country: "US", Admin1: "Oregon"},
		}
		got := geocodeToSearchResults(input)
		if len(got) != 1 {
			t.Fatalf("expected 1 result, got %d", len(got))
		}
		if got[0].Name != "Portland" {
			t.Errorf("Name = %q", got[0].Name)
		}
		if got[0].Detail != "Oregon, US" {
			t.Errorf("Detail = %q, want %q", got[0].Detail, "Oregon, US")
		}
		if got[0].FullName != "Portland, Oregon" {
			t.Errorf("FullName = %q, want %q", got[0].FullName, "Portland, Oregon")
		}
		if got[0].Lat != "45.5000" || got[0].Lon != "-122.6000" {
			t.Errorf("coordinates = %s, %s", got[0].Lat, got[0].Lon)
		}
	})

	t.Run("without admin1", func(t *testing.T) {
		input := []GeocodeResult{
			{Name: "London", Latitude: 51.5, Longitude: -0.1, Country: "UK"},
		}
		got := geocodeToSearchResults(input)
		if got[0].Detail != "UK" {
			t.Errorf("Detail = %q, want %q", got[0].Detail, "UK")
		}
		if got[0].FullName != "London" {
			t.Errorf("FullName = %q, want %q", got[0].FullName, "London")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := geocodeToSearchResults(nil)
		if len(got) != 0 {
			t.Errorf("expected empty, got %d results", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// Tier 2: Data transformation functions (testable with fixture data)
// ---------------------------------------------------------------------------

func TestCalcNWSPrecip(t *testing.T) {
	days := []DailyRow{
		{Date: "Mon 02/05", Precip: "40%"},
		{Date: "Tue 02/06", Precip: "10%"},
		{Date: "Wed 02/07", Precip: "80%"},
	}

	entries, total := calcNWSPrecip(days)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Total should reflect the max percentage
	if total != "80% max" {
		t.Errorf("total = %q, want %q", total, "80% max")
	}

	// Bar height should be proportional to max
	if entries[2].BarHeight != 20 { // 80/80 * 20
		t.Errorf("max precip bar height = %f, want 20", entries[2].BarHeight)
	}
	if entries[0].BarHeight != 10 { // 40/80 * 20
		t.Errorf("40%% bar height = %f, want 10", entries[0].BarHeight)
	}

	// DailyVal should be -1 (probability, not amount)
	for i, e := range entries {
		if e.DailyVal != -1 {
			t.Errorf("entries[%d].DailyVal = %f, want -1", i, e.DailyVal)
		}
		if e.CumulativeRaw != -1 {
			t.Errorf("entries[%d].CumulativeRaw = %f, want -1", i, e.CumulativeRaw)
		}
	}
}

func TestCalcPrecipAccum(t *testing.T) {
	data := &ForecastResponse{
		Timezone: "UTC",
		Daily: DailyData{
			Time:      []string{"2026-02-05", "2026-02-06", "2026-02-07"},
			PrecipSum: []float64{0.25, 0.00, 0.50},
			// Provide other slices at matching length to avoid index panics
			TempMax:      []float64{50, 55, 45},
			TempMin:      []float64{30, 35, 25},
			WindSpeedMax: []float64{10, 15, 20},
			WeatherCode:  []int{0, 0, 61},
		},
	}

	entries, total := calcPrecipAccum(data)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if total != "0.75\"" {
		t.Errorf("total = %q, want %q", total, "0.75\"")
	}

	// Check cumulative values
	if entries[0].CumulativeRaw != 0.25 {
		t.Errorf("cumulative[0] = %f, want 0.25", entries[0].CumulativeRaw)
	}
	if entries[1].CumulativeRaw != 0.25 {
		t.Errorf("cumulative[1] = %f, want 0.25", entries[1].CumulativeRaw)
	}
	if math.Abs(entries[2].CumulativeRaw-0.75) > 0.001 {
		t.Errorf("cumulative[2] = %f, want 0.75", entries[2].CumulativeRaw)
	}

	// Bar height: max daily is 0.50, so day 3 should have barHeight=20
	if entries[2].BarHeight != 20 {
		t.Errorf("max daily bar height = %f, want 20", entries[2].BarHeight)
	}
	// Day 1: 0.25/0.50 * 20 = 10
	if entries[0].BarHeight != 10 {
		t.Errorf("day 1 bar height = %f, want 10", entries[0].BarHeight)
	}
	// Day 2: 0/0.50 * 20 = 0
	if entries[1].BarHeight != 0 {
		t.Errorf("day 2 bar height = %f, want 0", entries[1].BarHeight)
	}
}

func TestTransformDaily_BarScaling(t *testing.T) {
	// Test that bar scaling works correctly independent of time.Now()
	data := &ForecastResponse{
		Timezone: "UTC",
		Daily: DailyData{
			Time:         []string{"2026-02-05", "2026-02-06", "2026-02-07"},
			TempMin:      []float64{20, 30, 40},
			TempMax:      []float64{50, 60, 70},
			PrecipSum:    []float64{0, 0.5, 0},
			WindSpeedMax: []float64{10, 15, 5},
			WeatherCode:  []int{0, 63, 1},
		},
	}

	rows, globalLow, globalHigh := transformDaily(data)

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if globalLow != 20 || globalHigh != 70 {
		t.Errorf("globals = (%d, %d), want (20, 70)", globalLow, globalHigh)
	}

	// First row: low=20, high=50. Range is 20-70 (50 degrees)
	// barLeft = (20-20)/50 * 100 = 0
	// barWidth = (50-20)/50 * 100 = 60
	if rows[0].BarLeft != 0 {
		t.Errorf("row[0].BarLeft = %f, want 0", rows[0].BarLeft)
	}
	if rows[0].BarWidth != 60 {
		t.Errorf("row[0].BarWidth = %f, want 60", rows[0].BarWidth)
	}

	// Condition mapping
	if rows[1].Condition != "Rain" {
		t.Errorf("row[1].Condition = %q, want %q", rows[1].Condition, "Rain")
	}
}

func TestAggregateNWSPrecipByDay(t *testing.T) {
	// Simulate gridpoint data with a period that doesn't span midnight
	val := 25.4 // 25.4mm = 1 inch
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{
			ValidTime: "2026-02-05T12:00:00+00:00/PT6H",
			Value:     &val,
		},
	}

	dates := []string{"2026-02-05", "2026-02-06"}
	result := aggregateNWSPrecipByDay(gridData, dates, "UTC")

	// 25.4mm / 25.4 = 1 inch, all on Feb 5
	if math.Abs(result["2026-02-05"]-1.0) > 0.01 {
		t.Errorf("feb 5 precip = %f, want ~1.0", result["2026-02-05"])
	}
	if result["2026-02-06"] != 0 {
		t.Errorf("feb 6 precip = %f, want 0", result["2026-02-06"])
	}
}

func TestAggregateNWSPrecipByDay_MidnightSpan(t *testing.T) {
	// 25.4mm over PT6H starting at 21:00 (9PM) — spans midnight
	// 3 hours in first day, 3 hours in second
	val := 25.4
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{
			ValidTime: "2026-02-05T21:00:00+00:00/PT6H",
			Value:     &val,
		},
	}

	dates := []string{"2026-02-05", "2026-02-06"}
	result := aggregateNWSPrecipByDay(gridData, dates, "UTC")

	// Should split: 3/6 = 0.5 inch each day
	if math.Abs(result["2026-02-05"]-0.5) > 0.01 {
		t.Errorf("feb 5 precip = %f, want ~0.5", result["2026-02-05"])
	}
	if math.Abs(result["2026-02-06"]-0.5) > 0.01 {
		t.Errorf("feb 6 precip = %f, want ~0.5", result["2026-02-06"])
	}
}

func TestDistributeQPFToHours(t *testing.T) {
	// 6mm over PT6H starting at 06:00 on Feb 5
	val := 6.0 // 6mm
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{
			ValidTime: "2026-02-05T06:00:00+00:00/PT6H",
			Value:     &val,
		},
	}

	result := distributeQPFToHours(gridData, "2026-02-05", "UTC")

	// 6mm / 6 hours = 1mm/hour = 1/25.4 inches/hour ≈ 0.03937
	expectedPerHour := 6.0 / 25.4 / 6.0
	for h := 6; h < 12; h++ {
		if math.Abs(result[h]-expectedPerHour) > 0.001 {
			t.Errorf("hour %d: got %f, want ~%f", h, result[h], expectedPerHour)
		}
	}

	// Hours outside the range should be 0
	if result[5] != 0 {
		t.Errorf("hour 5 should be 0, got %f", result[5])
	}
	if result[12] != 0 {
		t.Errorf("hour 12 should be 0, got %f", result[12])
	}
}

func TestDistributeQPFToHours_NilValue(t *testing.T) {
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{
			ValidTime: "2026-02-05T06:00:00+00:00/PT6H",
			Value:     nil,
		},
	}

	result := distributeQPFToHours(gridData, "2026-02-05", "UTC")
	if len(result) != 0 {
		t.Errorf("expected empty map for nil value, got %v", result)
	}
}
