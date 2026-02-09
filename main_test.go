package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func fixedTime() time.Time {
	return time.Date(2026, 2, 8, 15, 0, 0, 0, time.UTC)
}

// newTestServer creates a *server with a fixed clock and real templates.
func newTestServer(t *testing.T, opts ...func(*server)) *server {
	t.Helper()
	funcMap := template.FuncMap{
		"gt": func(a, b float64) bool { return a > b },
	}
	s := &server{
		now:       fixedTime,
		templates: template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html")),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// makeOpenMeteoForecastResponse creates a realistic Open-Meteo forecast response.
func makeOpenMeteoForecastResponse() ForecastResponse {
	hourlyTimes := make([]string, 0, 48)
	temps := make([]float64, 0, 48)
	apparent := make([]float64, 0, 48)
	precip := make([]float64, 0, 48)
	wind := make([]float64, 0, 48)
	windDir := make([]float64, 0, 48)
	codes := make([]int, 0, 48)

	base := time.Date(2026, 2, 8, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 48; h++ {
		t := base.Add(time.Duration(h) * time.Hour)
		hourlyTimes = append(hourlyTimes, t.Format("2006-01-02T15:04"))
		temps = append(temps, 30+float64(h%12))
		apparent = append(apparent, 25+float64(h%12))
		precip = append(precip, float64(h%3)*0.05)
		wind = append(wind, 5+float64(h%5))
		windDir = append(windDir, float64((h*30)%360))
		codes = append(codes, []int{0, 1, 3, 61, 0, 1, 3, 61, 0, 1, 3, 61}[h%12])
	}

	return ForecastResponse{
		Latitude:  40.71,
		Longitude: -74.01,
		Timezone:  "UTC",
		Elevation: 10.0,
		Hourly: HourlyData{
			Time:          hourlyTimes,
			Temperature:   temps,
			ApparentTemp:  apparent,
			Precipitation: precip,
			WindSpeed:     wind,
			WindDirection: windDir,
			WeatherCode:   codes,
		},
		Daily: DailyData{
			Time:         []string{"2026-02-08", "2026-02-09", "2026-02-10", "2026-02-11", "2026-02-12", "2026-02-13", "2026-02-14"},
			TempMax:      []float64{42, 45, 50, 48, 55, 52, 47},
			TempMin:      []float64{25, 28, 32, 30, 38, 35, 29},
			PrecipSum:    []float64{0.1, 0.0, 0.5, 0.3, 0.0, 0.2, 0.0},
			WindSpeedMax: []float64{15, 10, 20, 12, 8, 18, 11},
			WeatherCode:  []int{1, 0, 63, 61, 0, 3, 1},
		},
	}
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

// makeNWSPeriods creates NWS daily forecast periods.
func makeNWSDailyPeriods() []NWSPeriod {
	return []NWSPeriod{
		{Number: 1, Name: "Sunday", StartTime: "2026-02-08T06:00:00-05:00", IsDaytime: true, Temperature: 42, WindSpeed: "10 mph", WindDirection: "NW", ShortForecast: "Partly Cloudy", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(20)}},
		{Number: 2, Name: "Sunday Night", StartTime: "2026-02-08T18:00:00-05:00", IsDaytime: false, Temperature: 28, WindSpeed: "5 mph", WindDirection: "N", ShortForecast: "Clear", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(0)}},
		{Number: 3, Name: "Monday", StartTime: "2026-02-09T06:00:00-05:00", IsDaytime: true, Temperature: 45, WindSpeed: "12 mph", WindDirection: "SW", ShortForecast: "Sunny", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(10)}},
		{Number: 4, Name: "Monday Night", StartTime: "2026-02-09T18:00:00-05:00", IsDaytime: false, Temperature: 30, WindSpeed: "8 mph", WindDirection: "W", ShortForecast: "Mostly Clear", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(5)}},
		{Number: 5, Name: "Tuesday", StartTime: "2026-02-10T06:00:00-05:00", IsDaytime: true, Temperature: 50, WindSpeed: "15 mph", WindDirection: "S", ShortForecast: "Rain", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(80)}},
		{Number: 6, Name: "Tuesday Night", StartTime: "2026-02-10T18:00:00-05:00", IsDaytime: false, Temperature: 35, WindSpeed: "10 mph", WindDirection: "SE", ShortForecast: "Rain Likely", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(60)}},
	}
}

// makeNWSHourlyPeriods creates NWS hourly forecast periods.
func makeNWSHourlyPeriods() []NWSPeriod {
	var periods []NWSPeriod
	base := time.Date(2026, 2, 8, 10, 0, 0, 0, time.FixedZone("EST", -5*3600))
	for i := 0; i < 24; i++ {
		t := base.Add(time.Duration(i) * time.Hour)
		pct := (i * 5) % 100
		periods = append(periods, NWSPeriod{
			Number:        i + 1,
			StartTime:     t.Format(time.RFC3339),
			IsDaytime:     t.Hour() >= 6 && t.Hour() < 18,
			Temperature:   30 + i%15,
			WindSpeed:     fmt.Sprintf("%d mph", 5+i%10),
			WindDirection: "NW",
			ShortForecast: "Fair",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{intPtr(pct)},
		})
	}
	return periods
}

func makeNWSGridpointResponse() NWSGridpointResponse {
	var resp NWSGridpointResponse
	resp.Properties.QuantitativePrecipitation.Uom = "wmoUnit:mm"
	resp.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T12:00:00+00:00/PT6H", Value: floatPtr(2.54)},
		{ValidTime: "2026-02-09T06:00:00+00:00/PT6H", Value: floatPtr(0.0)},
		{ValidTime: "2026-02-10T12:00:00+00:00/PT6H", Value: floatPtr(12.7)},
	}
	return resp
}

// newNWSMockServer creates a mock NWS API server.
func newNWSMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = serverURL + "/daily-forecast"
		resp.Properties.ForecastHourly = serverURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		w.Header().Set("Content-Type", "application/geo+json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		w.Header().Set("Content-Type", "application/geo+json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		w.Header().Set("Content-Type", "application/geo+json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/gridpoints/", func(w http.ResponseWriter, r *http.Request) {
		resp := makeNWSGridpointResponse()
		w.Header().Set("Content-Type", "application/geo+json")
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// newOpenMeteoMockServer creates a mock Open-Meteo API server.
func newOpenMeteoMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := makeOpenMeteoForecastResponse()
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newGeocodeMockServer creates a mock geocoding API server.
func newGeocodeMockServer(t *testing.T, results []GeocodeResult) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GeocodeResponse{Results: results})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newNominatimMockServer creates a mock Nominatim API server.
func newNominatimMockServer(t *testing.T, result NominatimResult) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupAllMockServer returns a *server with all mock URLs configured.
func setupAllMockServer(t *testing.T) *server {
	t.Helper()
	nwsSrv := newNWSMockServer(t)
	meteoSrv := newOpenMeteoMockServer(t)
	geocodeResults := []GeocodeResult{
		{Name: "New York", Latitude: 40.7128, Longitude: -74.0060, Country: "US", Admin1: "New York"},
		{Name: "Newark", Latitude: 40.7357, Longitude: -74.1724, Country: "US", Admin1: "New Jersey"},
	}
	geocodeSrv := newGeocodeMockServer(t, geocodeResults)
	nominatimResult := NominatimResult{Name: "New York"}
	nominatimResult.Address.City = "New York"
	nominatimSrv := newNominatimMockServer(t, nominatimResult)

	return newTestServer(t, func(s *server) {
		s.nwsBaseURL = nwsSrv.URL
		s.openMeteoBaseURL = meteoSrv.URL
		s.geocodeBaseURL = geocodeSrv.URL
		s.nominatimBaseURL = nominatimSrv.URL
	})
}

// ---------------------------------------------------------------------------
// Tier 1: Pure utility functions
// ---------------------------------------------------------------------------

func TestWmoDescription(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "Clear"}, {1, "Mostly Clear"}, {2, "Partly Cloudy"}, {3, "Overcast"},
		{45, "Fog"}, {48, "Rime Fog"},
		{51, "Lt Drizzle"}, {53, "Drizzle"}, {55, "Hvy Drizzle"},
		{56, "Frz Drizzle"}, {57, "Hvy Frz Drzl"},
		{61, "Lt Rain"}, {63, "Rain"}, {65, "Hvy Rain"},
		{66, "Frz Rain"}, {67, "Hvy Frz Rain"},
		{71, "Lt Snow"}, {73, "Snow"}, {75, "Hvy Snow"}, {77, "Snow Grains"},
		{80, "Lt Showers"}, {81, "Showers"}, {82, "Hvy Showers"},
		{85, "Lt Snow Shwr"}, {86, "Hvy Snow Shwr"},
		{95, "Tstorm"}, {96, "Tstorm+Hail"}, {99, "Tstorm+Hvy Hail"},
		{-1, "Unknown"}, {100, "Unknown"}, {999, "Unknown"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("code_%d", tt.code), func(t *testing.T) {
			if got := wmoDescription(tt.code); got != tt.want {
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
		{0, "N"}, {45, "NE"}, {90, "E"}, {135, "SE"},
		{180, "S"}, {225, "SW"}, {270, "W"}, {315, "NW"},
		{360, "N"}, {22.5, "NNE"}, {348.75, "N"},
		{11.25, "NNE"}, // boundary: Round(0.5)=1
		{67.5, "ENE"}, {112.5, "ESE"}, {157.5, "SSE"},
		{202.5, "SSW"}, {247.5, "WSW"}, {292.5, "WNW"},
		{337.5, "NNW"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.2f", tt.degrees), func(t *testing.T) {
			if got := windDirLabel(tt.degrees); got != tt.want {
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
		{"very cold clamped", -10, "#4a9eff"},
		{"exactly first stop", 20, "#4a9eff"},
		{"exactly last stop", 95, "#ff3333"},
		{"very hot clamped", 120, "#ff3333"},
		{"midpoint 20-45", 32.5, "#95bfef"},
		{"midpoint 65-80", 72.5, "#ffa239"},
		{"exactly 45", 45, "#e0e0e0"},
		{"exactly 65", 65, "#ffd93d"},
		{"exactly 80", 80, "#ff6b35"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tempToColor(tt.temp); got != tt.want {
				t.Errorf("tempToColor(%.1f) = %q, want %q", tt.temp, got, tt.want)
			}
		})
	}
	// Verify format is always #rrggbb
	for temp := -20.0; temp <= 120.0; temp += 5 {
		c := tempToColor(temp)
		if len(c) != 7 || c[0] != '#' {
			t.Errorf("tempToColor(%.0f) = %q, bad format", temp, c)
		}
	}
}

func TestWindchill(t *testing.T) {
	tests := []struct {
		name       string
		temp, wind float64
		want       int
	}{
		{"warm temp bypass", 50, 10, 50},
		{"above 50 bypass", 60, 20, 60},
		{"low wind bypass", 30, 3, 30},
		{"low wind exact bypass", 30, 2, 30},
		{"moderate cold/wind", 30, 10, 21},
		{"cold and windy", 0, 15, -19},
		{"freezing with wind", 20, 20, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windchill(tt.temp, tt.wind); got != tt.want {
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
		{"10 mph", 10}, {"5 to 10 mph", 5}, {"0 mph", 0}, {"25 mph", 25}, {"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseWindSpeed(tt.input); got != tt.want {
				t.Errorf("parseWindSpeed(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

func TestHaversine(t *testing.T) {
	tests := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		wantMin, wantMax       float64
	}{
		{"same point", 40.71, -74.01, 40.71, -74.01, 0, 0.001},
		{"NYC to LA", 40.7128, -74.0060, 34.0522, -118.2437, 3930, 3960},
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
		{"PT1H", 1}, {"PT6H", 6}, {"PT12H", 12}, {"PT24H", 24},
		{"INVALID", 6}, {"PT30M", 6}, {"", 6},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseISO8601Duration(tt.input); got != tt.want {
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
	if got := getDailyRowForDate(days, "2026-02-06"); got == nil || got.High != 50 {
		t.Errorf("expected High=50, got %v", got)
	}
	if got := getDailyRowForDate(days, "2026-02-10"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	if got := getDailyRowForDate(nil, "2026-02-06"); got != nil {
		t.Errorf("expected nil for nil slice, got %v", got)
	}
}

func TestGetAdjacentDates(t *testing.T) {
	days := []DailyRow{
		{DateISO: "2026-02-05"},
		{DateISO: "2026-02-06"},
		{DateISO: "2026-02-07"},
	}
	prev, next := getAdjacentDates(days, "2026-02-06")
	if prev != "2026-02-05" || next != "2026-02-07" {
		t.Errorf("middle: prev=%q, next=%q", prev, next)
	}
	prev, next = getAdjacentDates(days, "2026-02-05")
	if prev != "" || next != "2026-02-06" {
		t.Errorf("first: prev=%q, next=%q", prev, next)
	}
	prev, next = getAdjacentDates(days, "2026-02-07")
	if prev != "2026-02-06" || next != "" {
		t.Errorf("last: prev=%q, next=%q", prev, next)
	}
	prev, next = getAdjacentDates(days, "2026-02-10")
	if prev != "" || next != "" {
		t.Errorf("missing: prev=%q, next=%q", prev, next)
	}
}

func TestGeocodeToSearchResults(t *testing.T) {
	t.Run("with admin1", func(t *testing.T) {
		input := []GeocodeResult{{Name: "Portland", Latitude: 45.5, Longitude: -122.6, Country: "US", Admin1: "Oregon"}}
		got := geocodeToSearchResults(input)
		if len(got) != 1 {
			t.Fatalf("expected 1 result, got %d", len(got))
		}
		if got[0].Detail != "Oregon, US" {
			t.Errorf("Detail = %q", got[0].Detail)
		}
		if got[0].FullName != "Portland, Oregon" {
			t.Errorf("FullName = %q", got[0].FullName)
		}
		if got[0].Lat != "45.5000" || got[0].Lon != "-122.6000" {
			t.Errorf("coords = %s, %s", got[0].Lat, got[0].Lon)
		}
	})
	t.Run("without admin1", func(t *testing.T) {
		got := geocodeToSearchResults([]GeocodeResult{{Name: "London", Latitude: 51.5, Longitude: -0.1, Country: "UK"}})
		if got[0].Detail != "UK" || got[0].FullName != "London" {
			t.Errorf("Detail=%q FullName=%q", got[0].Detail, got[0].FullName)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := geocodeToSearchResults(nil); len(got) != 0 {
			t.Errorf("expected empty, got %d", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// Tier 2: Fetch functions (using httptest)
// ---------------------------------------------------------------------------

func TestFetchJSON(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"name":"test","latitude":1.5}`)
		}))
		defer srv.Close()

		var result GeocodeResult
		if err := fetchJSON(srv.URL, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Name != "test" || result.Latitude != 1.5 {
			t.Errorf("got %+v", result)
		}
	})
	t.Run("non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		defer srv.Close()

		var result GeocodeResult
		err := fetchJSON(srv.URL, &result)
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Errorf("expected status error, got %v", err)
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `not json`)
		}))
		defer srv.Close()

		var result GeocodeResult
		if err := fetchJSON(srv.URL, &result); err == nil {
			t.Error("expected JSON decode error")
		}
	})
	t.Run("network error", func(t *testing.T) {
		var result GeocodeResult
		if err := fetchJSON("http://localhost:1/bad", &result); err == nil {
			t.Error("expected network error")
		}
	})
}

func TestFetchNWSJSON(t *testing.T) {
	t.Run("success with headers", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") != "WX-Weather-App/1.0 (github.com/spwg/weather)" {
				t.Error("missing User-Agent")
			}
			if r.Header.Get("Accept") != "application/geo+json" {
				t.Error("missing Accept header")
			}
			fmt.Fprint(w, `{"name":"test"}`)
		}))
		defer srv.Close()

		var result GeocodeResult
		if err := fetchNWSJSON(srv.URL, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
		}))
		defer srv.Close()

		var result GeocodeResult
		err := fetchNWSJSON(srv.URL, &result)
		if err == nil || !strings.Contains(err.Error(), "status 404") {
			t.Errorf("expected status error, got %v", err)
		}
	})
	t.Run("network error", func(t *testing.T) {
		var result GeocodeResult
		if err := fetchNWSJSON("http://localhost:1/bad", &result); err == nil {
			t.Error("expected network error")
		}
	})
}

func TestFetchForecast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/forecast") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := makeOpenMeteoForecastResponse()
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := newTestServer(t, func(s *server) { s.openMeteoBaseURL = srv.URL })
	data, err := s.fetchForecast("40.71", "-74.01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Latitude != 40.71 {
		t.Errorf("latitude = %f", data.Latitude)
	}
	if len(data.Hourly.Time) == 0 {
		t.Error("no hourly data")
	}
}

func TestFetchForecast_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	s := newTestServer(t, func(s *server) { s.openMeteoBaseURL = srv.URL })
	_, err := s.fetchForecast("40.71", "-74.01")
	if err == nil {
		t.Error("expected error")
	}
}

func TestFetchGeocode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/search") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(GeocodeResponse{
			Results: []GeocodeResult{{Name: "Portland", Latitude: 45.5, Longitude: -122.6}},
		})
	}))
	defer srv.Close()

	s := newTestServer(t, func(s *server) { s.geocodeBaseURL = srv.URL })
	data, err := s.fetchGeocode("Portland")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Results) != 1 || data.Results[0].Name != "Portland" {
		t.Errorf("unexpected results: %+v", data.Results)
	}
}

func TestFetchGeocode_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	s := newTestServer(t, func(s *server) { s.geocodeBaseURL = srv.URL })
	_, err := s.fetchGeocode("Portland")
	if err == nil {
		t.Error("expected error")
	}
}

func TestFetchNWSPoints(t *testing.T) {
	nwsSrv := newNWSMockServer(t)
	s := newTestServer(t, func(s *server) { s.nwsBaseURL = nwsSrv.URL })

	data, err := s.fetchNWSPoints("40.71", "-74.01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Properties.GridID != "OKX" {
		t.Errorf("gridId = %q", data.Properties.GridID)
	}
}

func TestFetchNWSPoints_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	s := newTestServer(t, func(s *server) { s.nwsBaseURL = srv.URL })
	_, err := s.fetchNWSPoints("40.71", "-74.01")
	if err == nil {
		t.Error("expected error")
	}
}

func TestFetchNWSForecast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	data, err := fetchNWSForecast(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Properties.Periods) != 6 {
		t.Errorf("got %d periods", len(data.Properties.Periods))
	}
}

func TestFetchNWSForecast_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	_, err := fetchNWSForecast(srv.URL)
	if err == nil {
		t.Error("expected error")
	}
}

func TestFetchNWSGridpoint(t *testing.T) {
	nwsSrv := newNWSMockServer(t)
	s := newTestServer(t, func(s *server) { s.nwsBaseURL = nwsSrv.URL })

	data, err := s.fetchNWSGridpoint("OKX", 33, 37)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Properties.QuantitativePrecipitation.Values) == 0 {
		t.Error("no QPF values")
	}
}

func TestFetchNWSGridpoint_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	s := newTestServer(t, func(s *server) { s.nwsBaseURL = srv.URL })
	_, err := s.fetchNWSGridpoint("OKX", 33, 37)
	if err == nil {
		t.Error("expected error")
	}
}

func TestReverseGeocode(t *testing.T) {
	t.Run("city", func(t *testing.T) {
		result := NominatimResult{Name: "Fallback"}
		result.Address.City = "New York"
		srv := newNominatimMockServer(t, result)
		s := newTestServer(t, func(s *server) { s.nominatimBaseURL = srv.URL })

		name, err := s.reverseGeocode("40.71", "-74.01")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "New York" {
			t.Errorf("name = %q, want %q", name, "New York")
		}
	})
	t.Run("town fallback", func(t *testing.T) {
		result := NominatimResult{Name: "Fallback"}
		result.Address.Town = "Smithtown"
		srv := newNominatimMockServer(t, result)
		s := newTestServer(t, func(s *server) { s.nominatimBaseURL = srv.URL })

		name, err := s.reverseGeocode("40.71", "-74.01")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "Smithtown" {
			t.Errorf("name = %q, want %q", name, "Smithtown")
		}
	})
	t.Run("village fallback", func(t *testing.T) {
		result := NominatimResult{Name: "Fallback"}
		result.Address.Village = "Hamlet"
		srv := newNominatimMockServer(t, result)
		s := newTestServer(t, func(s *server) { s.nominatimBaseURL = srv.URL })

		name, err := s.reverseGeocode("40.71", "-74.01")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "Hamlet" {
			t.Errorf("name = %q, want %q", name, "Hamlet")
		}
	})
	t.Run("name fallback", func(t *testing.T) {
		result := NominatimResult{Name: "SomeName"}
		srv := newNominatimMockServer(t, result)
		s := newTestServer(t, func(s *server) { s.nominatimBaseURL = srv.URL })

		name, err := s.reverseGeocode("40.71", "-74.01")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "SomeName" {
			t.Errorf("name = %q, want %q", name, "SomeName")
		}
	})
	t.Run("network error", func(t *testing.T) {
		s := newTestServer(t, func(s *server) { s.nominatimBaseURL = "http://localhost:1" })
		_, err := s.reverseGeocode("40.71", "-74.01")
		if err == nil {
			t.Error("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// Tier 3: Data transformation functions
// ---------------------------------------------------------------------------

func TestTransformHourly(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 14, 30, 0, 0, time.UTC)
		}
	})
	data := makeOpenMeteoForecastResponse()

	rows := s.transformHourly(&data)

	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
	if len(rows) > 12 {
		t.Errorf("got %d rows, max 12", len(rows))
	}
	// First row should be at hour 14 (the current hour is 14:30, truncated to 14:00)
	if rows[0].Time != "2 PM" {
		t.Errorf("first row time = %q, want %q", rows[0].Time, "2 PM")
	}
	// Check IsNow on the current hour
	if !rows[0].IsNow {
		t.Error("expected first row to be IsNow")
	}
	// Second row should NOT be IsNow
	if len(rows) > 1 && rows[1].IsNow {
		t.Error("second row should not be IsNow")
	}
}

func TestTransformHourly_InvalidTimezone(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 14, 0, 0, 0, time.UTC)
		}
	})
	data := makeOpenMeteoForecastResponse()
	data.Timezone = "Invalid/Zone"

	rows := s.transformHourly(&data)
	// Should fall back to UTC and still produce results
	if len(rows) == 0 {
		t.Error("expected rows with invalid timezone fallback to UTC")
	}
}

func TestTransformDaily(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)
		}
	})

	data := makeOpenMeteoForecastResponse()
	rows, globalLow, globalHigh := s.transformDaily(&data)

	if len(rows) != 7 {
		t.Fatalf("expected 7 rows, got %d", len(rows))
	}
	if globalLow != 25 || globalHigh != 55 {
		t.Errorf("globals = (%d, %d), want (25, 55)", globalLow, globalHigh)
	}
	// First day should be IsToday
	if !rows[0].IsToday {
		t.Error("first day should be IsToday")
	}
	if rows[1].IsToday {
		t.Error("second day should not be IsToday")
	}
	// Check bar scaling
	if rows[0].BarLeft < 0 || rows[0].BarLeft > 100 {
		t.Errorf("barLeft out of range: %f", rows[0].BarLeft)
	}
	if rows[0].BarWidth < 2 {
		t.Errorf("barWidth below minimum: %f", rows[0].BarWidth)
	}
}

func TestTransformDaily_ZeroRange(t *testing.T) {
	s := newTestServer(t)

	data := &ForecastResponse{
		Timezone: "UTC",
		Daily: DailyData{
			Time:         []string{"2026-02-08"},
			TempMin:      []float64{50},
			TempMax:      []float64{50}, // same as min
			PrecipSum:    []float64{0},
			WindSpeedMax: []float64{10},
			WeatherCode:  []int{0},
		},
	}
	rows, _, _ := s.transformDaily(data)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// barWidth should be at least 2 (minimum enforcement)
	if rows[0].BarWidth < 2 {
		t.Errorf("barWidth = %f, want >= 2", rows[0].BarWidth)
	}
}

func TestTransformDaily_InvalidTimezone(t *testing.T) {
	s := newTestServer(t)

	data := &ForecastResponse{
		Timezone: "Bad/Zone",
		Daily: DailyData{
			Time:         []string{"2026-02-08"},
			TempMin:      []float64{30},
			TempMax:      []float64{50},
			PrecipSum:    []float64{0.1},
			WindSpeedMax: []float64{10},
			WeatherCode:  []int{0},
		},
	}
	rows, _, _ := s.transformDaily(data)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with timezone fallback, got %d", len(rows))
	}
}

func TestTransformNWSHourly(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 15, 0, 0, 0, time.FixedZone("EST", -5*3600))
		}
	})

	periods := makeNWSHourlyPeriods()
	rows := s.transformNWSHourly(periods, "America/New_York")

	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
	if len(rows) > 12 {
		t.Errorf("got %d rows, max 12", len(rows))
	}
	// Check windchill is calculated
	for _, r := range rows {
		if r.FeelsLike == 0 && r.Temp != 0 {
			// just verify it's populated
		}
	}
	// Check PrecipRaw is -1 for NWS
	for _, r := range rows {
		if r.PrecipRaw != -1 {
			t.Errorf("PrecipRaw = %f, want -1 for NWS hourly", r.PrecipRaw)
		}
	}
}

func TestTransformNWSHourly_NilPrecipProbability(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 9, 0, 0, 0, time.FixedZone("EST", -5*3600))
		}
	})

	periods := []NWSPeriod{
		{
			StartTime:     "2026-02-08T10:00:00-05:00",
			IsDaytime:     true,
			Temperature:   40,
			WindSpeed:     "10 mph",
			WindDirection: "N",
			ShortForecast: "Fair",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{nil},
		},
	}
	rows := s.transformNWSHourly(periods, "America/New_York")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Precip != "0%" {
		t.Errorf("Precip = %q, want %q", rows[0].Precip, "0%")
	}
}

func TestTransformNWSHourly_InvalidTimezone(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 9, 0, 0, 0, time.UTC)
		}
	})

	periods := []NWSPeriod{
		{
			StartTime:     "2026-02-08T10:00:00+00:00",
			Temperature:   40,
			WindSpeed:     "10 mph",
			WindDirection: "N",
			ShortForecast: "Fair",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{intPtr(10)},
		},
	}
	rows := s.transformNWSHourly(periods, "Bad/Zone")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with fallback to UTC, got %d", len(rows))
	}
}

func TestTransformNWSDaily(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 12, 0, 0, 0, time.FixedZone("EST", -5*3600))
		}
	})

	periods := makeNWSDailyPeriods()
	rows, globalLow, globalHigh := s.transformNWSDaily(periods, "America/New_York")

	if len(rows) == 0 {
		t.Fatal("expected daily rows")
	}
	if len(rows) > 7 {
		t.Errorf("got %d rows, max 7", len(rows))
	}
	if globalLow > globalHigh {
		t.Errorf("globalLow=%d > globalHigh=%d", globalLow, globalHigh)
	}
	// Check first day is today
	if !rows[0].IsToday {
		t.Error("first day should be IsToday")
	}
	// Check bar scaling is valid
	for _, r := range rows {
		if r.BarLeft < 0 || r.BarLeft > 100 {
			t.Errorf("BarLeft out of range: %f", r.BarLeft)
		}
		if r.BarWidth < 2 {
			t.Errorf("BarWidth below minimum: %f", r.BarWidth)
		}
	}
}

func TestTransformNWSDaily_NightOnly(t *testing.T) {
	s := newTestServer(t)

	// Only nighttime period (no daytime high)
	periods := []NWSPeriod{
		{
			StartTime:     "2026-02-08T18:00:00-05:00",
			IsDaytime:     false,
			Temperature:   25,
			WindSpeed:     "5 mph",
			WindDirection: "N",
			ShortForecast: "Clear Night",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{intPtr(0)},
		},
	}
	rows, _, _ := s.transformNWSDaily(periods, "America/New_York")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// hasHigh is false, so high = low + 10
	if rows[0].High != rows[0].Low+10 {
		t.Errorf("expected high=low+10, got low=%d high=%d", rows[0].Low, rows[0].High)
	}
	// Condition should be from nighttime since no daytime
	if rows[0].Condition != "Clear Night" {
		t.Errorf("Condition = %q, want %q", rows[0].Condition, "Clear Night")
	}
}

func TestTransformNWSDaily_DayOnly(t *testing.T) {
	s := newTestServer(t)

	periods := []NWSPeriod{
		{
			StartTime:     "2026-02-08T06:00:00-05:00",
			IsDaytime:     true,
			Temperature:   45,
			WindSpeed:     "10 mph",
			WindDirection: "W",
			ShortForecast: "Sunny",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{intPtr(5)},
		},
	}
	rows, _, _ := s.transformNWSDaily(periods, "America/New_York")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// hasLow is false, so low = high - 10
	if rows[0].Low != rows[0].High-10 {
		t.Errorf("expected low=high-10, got low=%d high=%d", rows[0].Low, rows[0].High)
	}
}

func TestTransformNWSDaily_InvalidTimezone(t *testing.T) {
	s := newTestServer(t)

	periods := makeNWSDailyPeriods()
	rows, _, _ := s.transformNWSDaily(periods, "Bad/Zone")
	if len(rows) == 0 {
		t.Error("expected rows with invalid timezone fallback to UTC")
	}
}

func TestTransformNWSDaily_ZeroTempRange(t *testing.T) {
	s := newTestServer(t)

	periods := []NWSPeriod{
		{StartTime: "2026-02-08T06:00:00+00:00", IsDaytime: true, Temperature: 40, WindSpeed: "5 mph", WindDirection: "N", ShortForecast: "Fair", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(0)}},
		{StartTime: "2026-02-08T18:00:00+00:00", IsDaytime: false, Temperature: 40, WindSpeed: "5 mph", WindDirection: "N", ShortForecast: "Fair", ProbabilityOfPrecipitation: struct {
			Value *int `json:"value"`
		}{intPtr(0)}},
	}
	rows, _, _ := s.transformNWSDaily(periods, "UTC")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// tempRange = 0 → set to 1, barWidth should be >= 2
	if rows[0].BarWidth < 2 {
		t.Errorf("barWidth = %f, want >= 2", rows[0].BarWidth)
	}
}

func TestCalcPrecipAccum(t *testing.T) {
	data := &ForecastResponse{
		Timezone: "UTC",
		Daily: DailyData{
			Time:         []string{"2026-02-08", "2026-02-09", "2026-02-10"},
			PrecipSum:    []float64{0.25, 0.00, 0.50},
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
	if math.Abs(entries[2].CumulativeRaw-0.75) > 0.001 {
		t.Errorf("cumulative[2] = %f, want 0.75", entries[2].CumulativeRaw)
	}
	if entries[2].BarHeight != 20 {
		t.Errorf("max bar height = %f, want 20", entries[2].BarHeight)
	}
	if entries[1].BarHeight != 0 {
		t.Errorf("zero precip bar height = %f, want 0", entries[1].BarHeight)
	}
}

func TestCalcPrecipAccum_InvalidTimezone(t *testing.T) {
	data := &ForecastResponse{
		Timezone: "Bad/Zone",
		Daily: DailyData{
			Time:      []string{"2026-02-08"},
			PrecipSum: []float64{0.1},
		},
	}
	entries, _ := calcPrecipAccum(data)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with timezone fallback, got %d", len(entries))
	}
}

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
	if total != "80% max" {
		t.Errorf("total = %q, want %q", total, "80% max")
	}
	if entries[2].BarHeight != 20 {
		t.Errorf("max bar height = %f, want 20", entries[2].BarHeight)
	}
	if entries[0].BarHeight != 10 {
		t.Errorf("40%% bar height = %f, want 10", entries[0].BarHeight)
	}
	for i, e := range entries {
		if e.DailyVal != -1 || e.CumulativeRaw != -1 {
			t.Errorf("entries[%d]: DailyVal=%f CumulativeRaw=%f, both want -1", i, e.DailyVal, e.CumulativeRaw)
		}
	}
}

func TestCalcNWSPrecip_ZeroPrecip(t *testing.T) {
	days := []DailyRow{
		{Date: "Mon 02/05", Precip: "0%"},
	}
	entries, total := calcNWSPrecip(days)
	if total != "0% max" {
		t.Errorf("total = %q", total)
	}
	if entries[0].BarHeight != 0 {
		t.Errorf("bar height = %f, want 0", entries[0].BarHeight)
	}
}

func TestCalcNWSPrecipWithQPF(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)
		}
	})

	days := []DailyRow{
		{Date: "Sun 02/08"},
		{Date: "Mon 02/09"},
	}
	precipByDay := map[string]float64{
		"2026-02-08": 0.5,
		"2026-02-09": 1.0,
	}

	entries, total := s.calcNWSPrecipWithQPF(days, precipByDay, "UTC")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if total != "1.50\"" {
		t.Errorf("total = %q, want %q", total, "1.50\"")
	}
	// Second entry should have max bar height
	if entries[1].BarHeight != 20 {
		t.Errorf("max bar height = %f, want 20", entries[1].BarHeight)
	}
	// Cumulative check
	if math.Abs(entries[1].CumulativeRaw-1.5) > 0.001 {
		t.Errorf("cumulative = %f, want 1.5", entries[1].CumulativeRaw)
	}
}

func TestCalcNWSPrecipWithQPF_InvalidTimezone(t *testing.T) {
	s := newTestServer(t)

	days := []DailyRow{{Date: "Sun 02/08"}}
	precipByDay := map[string]float64{"2026-02-08": 0.5}

	entries, _ := s.calcNWSPrecipWithQPF(days, precipByDay, "Bad/Zone")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with fallback, got %d", len(entries))
	}
}

func TestAggregateNWSPrecipByDay(t *testing.T) {
	val := 25.4 // 1 inch
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T12:00:00+00:00/PT6H", Value: &val},
	}
	dates := []string{"2026-02-08", "2026-02-09"}
	result := aggregateNWSPrecipByDay(gridData, dates, "UTC")

	if math.Abs(result["2026-02-08"]-1.0) > 0.01 {
		t.Errorf("feb 8 precip = %f, want ~1.0", result["2026-02-08"])
	}
	if result["2026-02-09"] != 0 {
		t.Errorf("feb 9 precip = %f, want 0", result["2026-02-09"])
	}
}

func TestAggregateNWSPrecipByDay_MidnightSpan(t *testing.T) {
	val := 25.4
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T21:00:00+00:00/PT6H", Value: &val},
	}
	dates := []string{"2026-02-08", "2026-02-09"}
	result := aggregateNWSPrecipByDay(gridData, dates, "UTC")

	if math.Abs(result["2026-02-08"]-0.5) > 0.01 {
		t.Errorf("feb 8 precip = %f, want ~0.5", result["2026-02-08"])
	}
	if math.Abs(result["2026-02-09"]-0.5) > 0.01 {
		t.Errorf("feb 9 precip = %f, want ~0.5", result["2026-02-09"])
	}
}

func TestAggregateNWSPrecipByDay_NilValue(t *testing.T) {
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T12:00:00+00:00/PT6H", Value: nil},
	}
	dates := []string{"2026-02-08"}
	result := aggregateNWSPrecipByDay(gridData, dates, "UTC")
	if result["2026-02-08"] != 0 {
		t.Errorf("expected 0 for nil value, got %f", result["2026-02-08"])
	}
}

func TestAggregateNWSPrecipByDay_InvalidTimezone(t *testing.T) {
	val := 25.4
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T12:00:00+00:00/PT6H", Value: &val},
	}
	dates := []string{"2026-02-08"}
	result := aggregateNWSPrecipByDay(gridData, dates, "Bad/Zone")
	// Should fallback to UTC and still work
	if result["2026-02-08"] == 0 {
		t.Error("expected non-zero precip with timezone fallback")
	}
}

func TestAggregateNWSPrecipByDay_InvalidValidTime(t *testing.T) {
	val := 25.4
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "bad-format", Value: &val},                // no "/" separator
		{ValidTime: "bad/PT6H", Value: &val},                  // bad timestamp
		{ValidTime: "2026-02-08T12:00:00+00:00", Value: &val}, // no duration part (only 1 part after split)
	}
	dates := []string{"2026-02-08"}
	result := aggregateNWSPrecipByDay(gridData, dates, "UTC")
	// All entries should be skipped
	if result["2026-02-08"] != 0 {
		t.Errorf("expected 0 for all invalid entries, got %f", result["2026-02-08"])
	}
}

func TestDistributeQPFToHours(t *testing.T) {
	val := 6.0
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T06:00:00+00:00/PT6H", Value: &val},
	}
	result := distributeQPFToHours(gridData, "2026-02-08", "UTC")

	expected := 6.0 / 25.4 / 6.0
	for h := 6; h < 12; h++ {
		if math.Abs(result[h]-expected) > 0.001 {
			t.Errorf("hour %d: %f, want %f", h, result[h], expected)
		}
	}
	if result[5] != 0 {
		t.Errorf("hour 5 = %f, want 0", result[5])
	}
}

func TestDistributeQPFToHours_NilAndZero(t *testing.T) {
	zero := 0.0
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T06:00:00+00:00/PT6H", Value: nil},
		{ValidTime: "2026-02-08T12:00:00+00:00/PT6H", Value: &zero},
	}
	result := distributeQPFToHours(gridData, "2026-02-08", "UTC")
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestDistributeQPFToHours_InvalidTimezone(t *testing.T) {
	val := 6.0
	gridData := &NWSGridpointResponse{}
	gridData.Properties.QuantitativePrecipitation.Values = []struct {
		ValidTime string   `json:"validTime"`
		Value     *float64 `json:"value"`
	}{
		{ValidTime: "2026-02-08T06:00:00+00:00/PT6H", Value: &val},
	}
	result := distributeQPFToHours(gridData, "2026-02-08", "Bad/Zone")
	// Should fallback to UTC
	if len(result) == 0 {
		t.Error("expected results with timezone fallback")
	}
}

func TestTransformHourlyForDay(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 14, 0, 0, 0, time.UTC)
		}
	})

	data := makeOpenMeteoForecastResponse()
	rows := s.transformHourlyForDay(&data, "2026-02-08")

	if len(rows) == 0 {
		t.Fatal("expected rows for target date")
	}
	// Check IsNow for the current hour
	foundNow := false
	for _, r := range rows {
		if r.IsNow {
			foundNow = true
		}
	}
	if !foundNow {
		t.Error("expected one row with IsNow=true for today")
	}
}

func TestTransformHourlyForDay_FutureDate(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 14, 0, 0, 0, time.UTC)
		}
	})

	data := makeOpenMeteoForecastResponse()
	rows := s.transformHourlyForDay(&data, "2026-02-09")

	if len(rows) == 0 {
		t.Fatal("expected rows for future date")
	}
	// Future date should have all 24 hours (no skipping)
	if len(rows) != 24 {
		t.Errorf("expected 24 rows for future date, got %d", len(rows))
	}
	// IsNow should be false for future dates
	for _, r := range rows {
		if r.IsNow {
			t.Error("IsNow should be false for future date")
		}
	}
}

func TestTransformHourlyForDay_InvalidTimezone(t *testing.T) {
	s := newTestServer(t)

	data := makeOpenMeteoForecastResponse()
	data.Timezone = "Bad/Zone"
	rows := s.transformHourlyForDay(&data, "2026-02-09")
	if len(rows) == 0 {
		t.Error("expected rows with timezone fallback")
	}
}

func TestTransformNWSHourlyForDay(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 12, 0, 0, 0, time.FixedZone("EST", -5*3600))
		}
	})

	periods := makeNWSHourlyPeriods()
	rows := s.transformNWSHourlyForDay(periods, "America/New_York", "2026-02-08")

	if len(rows) == 0 {
		t.Fatal("expected rows for target date")
	}
	// PrecipRaw should be -1 for NWS
	for _, r := range rows {
		if r.PrecipRaw != -1 {
			t.Errorf("PrecipRaw = %f, want -1", r.PrecipRaw)
		}
	}
}

func TestTransformNWSHourlyForDay_NilPrecip(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 9, 0, 0, 0, time.FixedZone("EST", -5*3600))
		}
	})

	periods := []NWSPeriod{
		{
			StartTime:     "2026-02-08T10:00:00-05:00",
			Temperature:   40,
			WindSpeed:     "10 mph",
			WindDirection: "N",
			ShortForecast: "Fair",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{nil},
		},
	}
	rows := s.transformNWSHourlyForDay(periods, "America/New_York", "2026-02-08")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Precip != "0%" {
		t.Errorf("Precip = %q, want %q", rows[0].Precip, "0%")
	}
}

func TestTransformNWSHourlyForDay_InvalidTimezone(t *testing.T) {
	s := newTestServer(t, func(s *server) {
		s.now = func() time.Time {
			return time.Date(2026, 2, 8, 9, 0, 0, 0, time.UTC)
		}
	})

	periods := []NWSPeriod{
		{
			StartTime:     "2026-02-08T10:00:00+00:00",
			Temperature:   40,
			WindSpeed:     "10 mph",
			WindDirection: "N",
			ShortForecast: "Fair",
			ProbabilityOfPrecipitation: struct {
				Value *int `json:"value"`
			}{intPtr(0)},
		},
	}
	rows := s.transformNWSHourlyForDay(periods, "Bad/Zone", "2026-02-08")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with fallback, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Tier 4: HTTP handler tests
// ---------------------------------------------------------------------------

func TestHandleIndex(t *testing.T) {
	s := newTestServer(t)

	t.Run("root path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		s.handleIndex(w, req)
		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), "WX") {
			t.Error("expected WX title in body")
		}
	})
	t.Run("non-root 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/other", nil)
		w := httptest.NewRecorder()
		s.handleIndex(w, req)
		if w.Code != 404 {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

func TestHandleSearch(t *testing.T) {
	s := newTestServer(t)

	t.Run("short query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q=a", nil)
		w := httptest.NewRecorder()
		s.handleSearch(w, req)
		if w.Body.String() != "" {
			t.Errorf("expected empty body, got %q", w.Body.String())
		}
	})
	t.Run("empty query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q=", nil)
		w := httptest.NewRecorder()
		s.handleSearch(w, req)
		if w.Body.String() != "" {
			t.Errorf("expected empty body, got %q", w.Body.String())
		}
	})
	t.Run("valid query with results", func(t *testing.T) {
		geocodeSrv := newGeocodeMockServer(t, []GeocodeResult{
			{Name: "Portland", Latitude: 45.5, Longitude: -122.6, Country: "US", Admin1: "Oregon"},
		})
		s := newTestServer(t, func(s *server) { s.geocodeBaseURL = geocodeSrv.URL })

		req := httptest.NewRequest("GET", "/search?q=Portland", nil)
		w := httptest.NewRecorder()
		s.handleSearch(w, req)
		if !strings.Contains(w.Body.String(), "Portland") {
			t.Error("expected Portland in response")
		}
	})
	t.Run("no results", func(t *testing.T) {
		geocodeSrv := newGeocodeMockServer(t, nil)
		s := newTestServer(t, func(s *server) { s.geocodeBaseURL = geocodeSrv.URL })

		req := httptest.NewRequest("GET", "/search?q=xyzxyz", nil)
		w := httptest.NewRecorder()
		s.handleSearch(w, req)
		if !strings.Contains(w.Body.String(), "No results") {
			t.Error("expected 'No results' in response")
		}
	})
	t.Run("geocode error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		defer srv.Close()
		s := newTestServer(t, func(s *server) { s.geocodeBaseURL = srv.URL })

		req := httptest.NewRequest("GET", "/search?q=Portland", nil)
		w := httptest.NewRecorder()
		s.handleSearch(w, req)
		if !strings.Contains(w.Body.String(), "No results") {
			t.Error("expected 'No results' on error")
		}
	})
}

func TestHandleForecast_MissingParams(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/forecast", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if !strings.Contains(w.Body.String(), "Missing coordinates") {
		t.Error("expected 'Missing coordinates' error")
	}
}

func TestHandleForecast_OpenMeteoPath(t *testing.T) {
	s := setupAllMockServer(t)
	// Make NWS fail so we fall through to Open-Meteo
	nwsFailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer nwsFailSrv.Close()
	s.nwsBaseURL = nwsFailSrv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=51.5&lon=-0.1&name=London", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "forecast") {
		t.Error("expected forecast content")
	}
}

func TestHandleForecast_OpenMeteoPathNoName(t *testing.T) {
	s := setupAllMockServer(t)
	nwsFailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer nwsFailSrv.Close()
	s.nwsBaseURL = nwsFailSrv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=51.5&lon=-0.1", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleForecast_OpenMeteoError(t *testing.T) {
	s := setupAllMockServer(t)
	// Both NWS and Open-Meteo fail
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer failSrv.Close()
	s.nwsBaseURL = failSrv.URL
	s.openMeteoBaseURL = failSrv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=51.5&lon=-0.1", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "Weather data unavailable") {
		t.Error("expected error message when both APIs fail")
	}
}

func TestHandleForecast_NWSPath(t *testing.T) {
	s := setupAllMockServer(t)

	req := httptest.NewRequest("GET", "/forecast?lat=40.71&lon=-74.01&name=New+York", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "forecast") {
		t.Error("expected forecast content")
	}
}

func TestHandleForecast_NWSPathNoName(t *testing.T) {
	s := setupAllMockServer(t)

	req := httptest.NewRequest("GET", "/forecast?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleForecast_NWSGridpointError(t *testing.T) {
	s := setupAllMockServer(t)
	// NWS points and forecast succeed but gridpoint fails
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/gridpoints/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500) // gridpoint fails
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=40.71&lon=-74.01&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	// Should fall back to probability-based precip (calcNWSPrecip)
	body := w.Body.String()
	if !strings.Contains(body, "forecast") {
		t.Error("expected forecast content even with gridpoint error")
	}
}

func TestHandleForecast_NWSEmptyTimezone(t *testing.T) {
	s := setupAllMockServer(t)
	// NWS with empty timezone should default to America/New_York
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "" // empty timezone
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/gridpoints/", func(w http.ResponseWriter, r *http.Request) {
		resp := makeNWSGridpointResponse()
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=40.71&lon=-74.01&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleForecast_NWSDailyError_FallbackToOpenMeteo(t *testing.T) {
	s := setupAllMockServer(t)
	// NWS points succeed but daily forecast fails
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=40.71&lon=-74.01&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleForecast_NWSHourlyError_FallbackToOpenMeteo(t *testing.T) {
	s := setupAllMockServer(t)
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/forecast?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_MissingParams(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/day-forecast", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if !strings.Contains(w.Body.String(), "Missing parameters") {
		t.Error("expected 'Missing parameters' error")
	}
}

func TestHandleDayForecast_InvalidDate(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=bad-date", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if !strings.Contains(w.Body.String(), "Invalid date format") {
		t.Error("expected 'Invalid date format' error")
	}
}

func TestHandleDayForecast_OpenMeteoPath(t *testing.T) {
	s := setupAllMockServer(t)
	nwsFailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer nwsFailSrv.Close()
	s.nwsBaseURL = nwsFailSrv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=51.5&lon=-0.1&day=2026-02-09&name=London", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "day-forecast") {
		t.Error("expected day-forecast content")
	}
}

func TestHandleDayForecast_OpenMeteoPathNoName(t *testing.T) {
	s := setupAllMockServer(t)
	nwsFailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer nwsFailSrv.Close()
	s.nwsBaseURL = nwsFailSrv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=51.5&lon=-0.1&day=2026-02-09", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_OpenMeteoError(t *testing.T) {
	s := setupAllMockServer(t)
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer failSrv.Close()
	s.nwsBaseURL = failSrv.URL
	s.openMeteoBaseURL = failSrv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=51.5&lon=-0.1&day=2026-02-09", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if !strings.Contains(w.Body.String(), "Weather data unavailable") {
		t.Error("expected error when both APIs fail")
	}
}

func TestHandleDayForecast_OpenMeteoDayNotFound(t *testing.T) {
	s := setupAllMockServer(t)
	nwsFailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer nwsFailSrv.Close()
	s.nwsBaseURL = nwsFailSrv.URL

	// Request a day not in the 7-day forecast
	req := httptest.NewRequest("GET", "/day-forecast?lat=51.5&lon=-0.1&day=2026-03-01", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if !strings.Contains(w.Body.String(), "Day not found") {
		t.Error("expected 'Day not found' error for date outside forecast range")
	}
}

func TestHandleDayForecast_NWSPath(t *testing.T) {
	s := setupAllMockServer(t)

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-02-08&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_NWSPathNoName(t *testing.T) {
	s := setupAllMockServer(t)

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-02-08", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_NWSEmptyTimezone(t *testing.T) {
	s := setupAllMockServer(t)
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = ""
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/gridpoints/", func(w http.ResponseWriter, r *http.Request) {
		resp := makeNWSGridpointResponse()
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-02-08&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_NWSDayNotFound(t *testing.T) {
	s := setupAllMockServer(t)

	// Request a day not in the NWS forecast range
	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-03-01&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if !strings.Contains(w.Body.String(), "Day not found") {
		t.Error("expected 'Day not found' error")
	}
}

func TestHandleDayForecast_NWSDailyError_Fallback(t *testing.T) {
	s := setupAllMockServer(t)
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-02-09&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_NWSHourlyError_Fallback(t *testing.T) {
	s := setupAllMockServer(t)
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-02-09&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleDayForecast_NWSGridpointError(t *testing.T) {
	s := setupAllMockServer(t)
	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/points/", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSPointsResponse{}
		resp.Properties.Forecast = srvURL + "/daily-forecast"
		resp.Properties.ForecastHourly = srvURL + "/hourly-forecast"
		resp.Properties.TimeZone = "America/New_York"
		resp.Properties.GridID = "OKX"
		resp.Properties.GridX = 33
		resp.Properties.GridY = 37
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/daily-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSDailyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/hourly-forecast", func(w http.ResponseWriter, r *http.Request) {
		resp := NWSForecastResponse{}
		resp.Properties.Periods = makeNWSHourlyPeriods()
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/gridpoints/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	s.nwsBaseURL = srv.URL

	req := httptest.NewRequest("GET", "/day-forecast?lat=40.71&lon=-74.01&day=2026-02-08&name=NYC", nil)
	w := httptest.NewRecorder()
	s.handleDayForecast(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNearby_MissingCoordinates(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/nearby", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	if !strings.Contains(w.Body.String(), "Missing coordinates") {
		t.Error("expected 'Missing coordinates' error")
	}
}

func TestHandleNearby_Success(t *testing.T) {
	s := setupAllMockServer(t)

	req := httptest.NewRequest("GET", "/nearby?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "nearby-picker") {
		t.Error("expected nearby picker template content")
	}
}

func TestHandleNearby_ReverseGeocodeError(t *testing.T) {
	s := setupAllMockServer(t)
	// Make nominatim fail
	s.nominatimBaseURL = "http://localhost:1"

	req := httptest.NewRequest("GET", "/nearby?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	// Should fall back to handleForecast
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNearby_EmptyCityName(t *testing.T) {
	s := setupAllMockServer(t)
	// Nominatim returns empty result
	emptyResult := NominatimResult{}
	nominatimSrv := newNominatimMockServer(t, emptyResult)
	s.nominatimBaseURL = nominatimSrv.URL

	req := httptest.NewRequest("GET", "/nearby?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	// Empty city name → falls back to handleForecast
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNearby_GeocodeNoResults(t *testing.T) {
	s := setupAllMockServer(t)
	// Geocode returns empty results
	geocodeSrv := newGeocodeMockServer(t, nil)
	s.geocodeBaseURL = geocodeSrv.URL

	req := httptest.NewRequest("GET", "/nearby?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	// No results → falls back to handleForecast
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNearby_InvalidCoordinates(t *testing.T) {
	s := setupAllMockServer(t)

	req := httptest.NewRequest("GET", "/nearby?lat=notnum&lon=notnum", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	// Invalid lat/lon parse → falls back to handleForecast
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNearby_NoCitiesWithin200km(t *testing.T) {
	s := setupAllMockServer(t)
	// Return cities that are far away (>200km)
	geocodeSrv := newGeocodeMockServer(t, []GeocodeResult{
		{Name: "FarCity", Latitude: 0, Longitude: 0, Country: "XX"}, // very far from 40.71,-74.01
	})
	s.geocodeBaseURL = geocodeSrv.URL

	req := httptest.NewRequest("GET", "/nearby?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	// No nearby cities → falls back to handleForecast
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNearby_GeocodeError(t *testing.T) {
	s := setupAllMockServer(t)
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer failSrv.Close()
	s.geocodeBaseURL = failSrv.URL

	req := httptest.NewRequest("GET", "/nearby?lat=40.71&lon=-74.01", nil)
	w := httptest.NewRecorder()
	s.handleNearby(w, req)
	// Geocode error → falls back to handleForecast
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
