package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Open-Meteo API response structs

type ForecastResponse struct {
	Latitude  float64    `json:"latitude"`
	Longitude float64    `json:"longitude"`
	Timezone  string     `json:"timezone"`
	Elevation float64    `json:"elevation"`
	Hourly    HourlyData `json:"hourly"`
	Daily     DailyData  `json:"daily"`
}

type HourlyData struct {
	Time              []string  `json:"time"`
	Temperature       []float64 `json:"temperature_2m"`
	ApparentTemp      []float64 `json:"apparent_temperature"`
	Precipitation     []float64 `json:"precipitation"`
	WindSpeed         []float64 `json:"wind_speed_10m"`
	WindDirection     []float64 `json:"wind_direction_10m"`
	WeatherCode       []int     `json:"weather_code"`
}

type DailyData struct {
	Time             []string  `json:"time"`
	TempMax          []float64 `json:"temperature_2m_max"`
	TempMin          []float64 `json:"temperature_2m_min"`
	PrecipSum        []float64 `json:"precipitation_sum"`
	WindSpeedMax     []float64 `json:"wind_speed_10m_max"`
	WeatherCode      []int     `json:"weather_code"`
}

type GeocodeResponse struct {
	Results []GeocodeResult `json:"results"`
}

type GeocodeResult struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Country   string  `json:"country"`
	Admin1    string  `json:"admin1"`
}

// NWS API response structs

type NWSPointsResponse struct {
	Properties struct {
		Forecast       string `json:"forecast"`
		ForecastHourly string `json:"forecastHourly"`
		TimeZone       string `json:"timeZone"`
		GridId         string `json:"gridId"`
		GridX          int    `json:"gridX"`
		GridY          int    `json:"gridY"`
	} `json:"properties"`
}

type NWSForecastResponse struct {
	Properties struct {
		Periods []NWSPeriod `json:"periods"`
	} `json:"properties"`
}

type NWSPeriod struct {
	Number                     int    `json:"number"`
	Name                       string `json:"name"`
	StartTime                  string `json:"startTime"`
	IsDaytime                  bool   `json:"isDaytime"`
	Temperature                int    `json:"temperature"`
	WindSpeed                  string `json:"windSpeed"`
	WindDirection              string `json:"windDirection"`
	ShortForecast              string `json:"shortForecast"`
	ProbabilityOfPrecipitation struct {
		Value *int `json:"value"`
	} `json:"probabilityOfPrecipitation"`
}

type NWSGridpointResponse struct {
	Properties struct {
		QuantitativePrecipitation struct {
			Uom    string `json:"uom"`
			Values []struct {
				ValidTime string   `json:"validTime"`
				Value     *float64 `json:"value"`
			} `json:"values"`
		} `json:"quantitativePrecipitation"`
	} `json:"properties"`
}

// View-model structs for templates

type HourlyRow struct {
	Time      string
	Temp      int
	FeelsLike int
	Precip    string
	PrecipRaw float64 // raw precipitation in inches, -1 if probability
	WindSpeed int
	WindDir   string
	Condition string
	IsNow     bool
}

type DailyRow struct {
	Date      string
	DateISO   string // YYYY-MM-DD format for URL state
	Low       int
	High      int
	LowColor  string
	HighColor string
	BarLeft   float64
	BarWidth  float64
	Precip    string
	PrecipRaw float64 // raw precipitation in inches, -1 if probability
	Wind      int
	Condition string
	IsToday   bool
}

type PrecipEntry struct {
	Day           string
	Daily         string
	DailyVal      float64
	Cumulative    string
	CumulativeRaw float64 // raw cumulative in inches, -1 if not applicable
	BarHeight     float64
}

type ForecastData struct {
	Name           string
	Lat            string
	Lon            string
	Timezone       string
	Elevation      int
	Hours          []HourlyRow
	MoreHours      []HourlyRow
	MoreCount      int
	Days           []DailyRow
	Precip         []PrecipEntry
	TotalPrecip    string
	TotalPrecipRaw float64 // raw total in inches, -1 if not applicable
	GlobalLow      int
	GlobalHigh     int
}

type DayForecastData struct {
	Name      string
	Lat       string
	Lon       string
	Timezone  string
	Date      string      // "Wednesday, February 5"
	DateShort string      // "Wed 02/05"
	DateISO   string      // "2026-02-05"
	Summary   DailyRow    // Day overview
	Hours     []HourlyRow // All hours for this day
	PrevDay   string      // Previous day ISO date (empty if none)
	NextDay   string      // Next day ISO date (empty if none)
}

type SearchData struct {
	Results []SearchResult
	Query   string
}

type SearchResult struct {
	Name    string
	Detail  string
	Lat     string
	Lon     string
	FullName string
}

type ErrorData struct {
	Message string
}

type BaseData struct {
	DevMode bool
}

type NominatimResult struct {
	Name    string `json:"name"`
	Address struct {
		City    string `json:"city"`
		Town    string `json:"town"`
		Village string `json:"village"`
	} `json:"address"`
}

type NearbyData struct {
	Results []SearchResult
	Lat     string
	Lon     string
}

type server struct {
	now              func() time.Time
	openMeteoBaseURL string
	geocodeBaseURL   string
	nwsBaseURL       string
	nominatimBaseURL string
	templates        *template.Template
}

var wmoDescriptions = map[int]string{
	0: "Clear", 1: "Mostly Clear", 2: "Partly Cloudy", 3: "Overcast",
	45: "Fog", 48: "Rime Fog",
	51: "Lt Drizzle", 53: "Drizzle", 55: "Hvy Drizzle",
	56: "Frz Drizzle", 57: "Hvy Frz Drzl",
	61: "Lt Rain", 63: "Rain", 65: "Hvy Rain",
	66: "Frz Rain", 67: "Hvy Frz Rain",
	71: "Lt Snow", 73: "Snow", 75: "Hvy Snow", 77: "Snow Grains",
	80: "Lt Showers", 81: "Showers", 82: "Hvy Showers",
	85: "Lt Snow Shwr", 86: "Hvy Snow Shwr",
	95: "Tstorm", 96: "Tstorm+Hail", 99: "Tstorm+Hvy Hail",
}

func wmoDescription(code int) string {
	if desc, ok := wmoDescriptions[code]; ok {
		return desc
	}
	return "Unknown"
}

func windDirLabel(degrees float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	ix := int(math.Round(degrees/22.5)) % 16
	return dirs[ix]
}

// tempToColor maps a temperature (Fahrenheit) to a hex color string.
// Color scale: Blue (cold) → White (mild) → Yellow → Orange → Red (hot)
func tempToColor(temp float64) string {
	type colorStop struct {
		temp float64
		r, g, b uint8
	}
	stops := []colorStop{
		{20, 0x4a, 0x9e, 0xff},  // Blue - cold
		{45, 0xe0, 0xe0, 0xe0},  // White - mild
		{65, 0xff, 0xd9, 0x3d},  // Yellow - warm
		{80, 0xff, 0x6b, 0x35},  // Orange - hot
		{95, 0xff, 0x33, 0x33},  // Red - very hot
	}

	// Clamp to range
	if temp <= stops[0].temp {
		return fmt.Sprintf("#%02x%02x%02x", stops[0].r, stops[0].g, stops[0].b)
	}
	if temp >= stops[len(stops)-1].temp {
		s := stops[len(stops)-1]
		return fmt.Sprintf("#%02x%02x%02x", s.r, s.g, s.b)
	}

	// Find the two stops to interpolate between
	for i := 0; i < len(stops)-1; i++ {
		if temp >= stops[i].temp && temp <= stops[i+1].temp {
			t := (temp - stops[i].temp) / (stops[i+1].temp - stops[i].temp)
			r := uint8(float64(stops[i].r) + t*(float64(stops[i+1].r)-float64(stops[i].r)))
			g := uint8(float64(stops[i].g) + t*(float64(stops[i+1].g)-float64(stops[i].g)))
			b := uint8(float64(stops[i].b) + t*(float64(stops[i+1].b)-float64(stops[i].b)))
			return fmt.Sprintf("#%02x%02x%02x", r, g, b)
		}
	}
	return "#e0e0e0" // fallback
}

// windchill calculates the wind chill temperature using NWS formula.
// Returns actual temp if conditions don't apply (temp >= 50°F or wind <= 3 mph).
func windchill(tempF float64, windMph float64) int {
	if tempF >= 50 || windMph <= 3 {
		return int(math.Round(tempF))
	}
	// NWS Wind Chill formula
	wc := 35.74 + 0.6215*tempF - 35.75*math.Pow(windMph, 0.16) + 0.4275*tempF*math.Pow(windMph, 0.16)
	return int(math.Round(wc))
}

// parseWindSpeed extracts mph value from NWS wind string like "10 mph" or "5 to 10 mph"
func parseWindSpeed(windStr string) float64 {
	// Try to find the first number
	var speed float64
	fmt.Sscanf(windStr, "%f", &speed)
	return speed
}

func fetchJSON(apiURL string, target any) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *server) fetchForecast(lat, lon string) (*ForecastResponse, error) {
	apiURL := fmt.Sprintf(
		s.openMeteoBaseURL+"/v1/forecast?latitude=%s&longitude=%s"+
			"&hourly=temperature_2m,apparent_temperature,precipitation,wind_speed_10m,wind_direction_10m,weather_code"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_sum,wind_speed_10m_max,weather_code"+
			"&temperature_unit=fahrenheit&wind_speed_unit=mph&precipitation_unit=inch"+
			"&timezone=auto&forecast_days=7",
		url.QueryEscape(lat), url.QueryEscape(lon),
	)
	var data ForecastResponse
	if err := fetchJSON(apiURL, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *server) fetchGeocode(query string) (*GeocodeResponse, error) {
	apiURL := fmt.Sprintf(
		s.geocodeBaseURL+"/v1/search?name=%s&count=5&language=en&format=json",
		url.QueryEscape(query),
	)
	var data GeocodeResponse
	if err := fetchJSON(apiURL, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// NWS API requires User-Agent header
func fetchNWSJSON(apiURL string, target any) error {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "WX-Weather-App/1.0 (github.com/spwg/weather)")
	req.Header.Set("Accept", "application/geo+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("NWS request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("NWS API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (s *server) fetchNWSPoints(lat, lon string) (*NWSPointsResponse, error) {
	apiURL := fmt.Sprintf(s.nwsBaseURL+"/points/%s,%s", lat, lon)
	var data NWSPointsResponse
	if err := fetchNWSJSON(apiURL, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func fetchNWSForecast(forecastURL string) (*NWSForecastResponse, error) {
	var data NWSForecastResponse
	if err := fetchNWSJSON(forecastURL, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *server) fetchNWSGridpoint(gridId string, gridX, gridY int) (*NWSGridpointResponse, error) {
	apiURL := fmt.Sprintf(s.nwsBaseURL+"/gridpoints/%s/%d,%d", gridId, gridX, gridY)
	var data NWSGridpointResponse
	if err := fetchNWSJSON(apiURL, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *server) reverseGeocode(lat, lon string) (string, error) {
	apiURL := fmt.Sprintf(
		s.nominatimBaseURL+"/reverse?lat=%s&lon=%s&format=json&zoom=10",
		url.QueryEscape(lat), url.QueryEscape(lon),
	)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "WX-Weather-App/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("nominatim request failed: %w", err)
	}
	defer resp.Body.Close()

	var result NominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	name := result.Address.City
	if name == "" {
		name = result.Address.Town
	}
	if name == "" {
		name = result.Address.Village
	}
	if name == "" {
		name = result.Name
	}
	return name, nil
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth radius in km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func geocodeToSearchResults(results []GeocodeResult) []SearchResult {
	var out []SearchResult
	for _, r := range results {
		detail := r.Country
		if r.Admin1 != "" {
			detail = r.Admin1 + ", " + r.Country
		}
		fullName := r.Name
		if r.Admin1 != "" {
			fullName = r.Name + ", " + r.Admin1
		}
		out = append(out, SearchResult{
			Name:     r.Name,
			Detail:   detail,
			Lat:      fmt.Sprintf("%.4f", r.Latitude),
			Lon:      fmt.Sprintf("%.4f", r.Longitude),
			FullName: fullName,
		})
	}
	return out
}

func (s *server) transformHourly(data *ForecastResponse) []HourlyRow {
	loc, err := time.LoadLocation(data.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := s.now().In(loc)

	var rows []HourlyRow
	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		if t.Before(now.Truncate(time.Hour)) {
			continue
		}
		if len(rows) >= 12 {
			break
		}
		rows = append(rows, HourlyRow{
			Time:      t.Format("3 PM"),
			Temp:      int(math.Round(data.Hourly.Temperature[i])),
			FeelsLike: int(math.Round(data.Hourly.ApparentTemp[i])),
			Precip:    fmt.Sprintf("%.2f\"", data.Hourly.Precipitation[i]),
			PrecipRaw: data.Hourly.Precipitation[i],
			WindSpeed: int(math.Round(data.Hourly.WindSpeed[i])),
			WindDir:   windDirLabel(data.Hourly.WindDirection[i]),
			Condition: wmoDescription(data.Hourly.WeatherCode[i]),
			IsNow:     t.Hour() == now.Hour(),
		})
	}
	return rows
}

func (s *server) transformDaily(data *ForecastResponse) ([]DailyRow, int, int) {
	loc, err := time.LoadLocation(data.Timezone)
	if err != nil {
		loc = time.UTC
	}
	today := s.now().In(loc).Format("2006-01-02")

	// Find global min/max for bar scaling
	globalMin := math.Inf(1)
	globalMax := math.Inf(-1)
	for _, v := range data.Daily.TempMin {
		if v < globalMin {
			globalMin = v
		}
	}
	for _, v := range data.Daily.TempMax {
		if v > globalMax {
			globalMax = v
		}
	}
	tempRange := globalMax - globalMin
	if tempRange == 0 {
		tempRange = 1
	}

	var rows []DailyRow
	for i, dateStr := range data.Daily.Time {
		t, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			continue
		}
		low := data.Daily.TempMin[i]
		high := data.Daily.TempMax[i]
		barLeft := ((low - globalMin) / tempRange) * 100
		barWidth := ((high - low) / tempRange) * 100
		if barWidth < 2 {
			barWidth = 2
		}

		rows = append(rows, DailyRow{
			Date:      t.Format("Mon 01/02"),
			DateISO:   dateStr,
			Low:       int(math.Round(low)),
			High:      int(math.Round(high)),
			LowColor:  tempToColor(low),
			HighColor: tempToColor(high),
			BarLeft:   math.Round(barLeft*10) / 10,
			BarWidth:  math.Round(barWidth*10) / 10,
			Precip:    fmt.Sprintf("%.2f\"", data.Daily.PrecipSum[i]),
			PrecipRaw: data.Daily.PrecipSum[i],
			Wind:      int(math.Round(data.Daily.WindSpeedMax[i])),
			Condition: wmoDescription(data.Daily.WeatherCode[i]),
			IsToday:   dateStr == today,
		})
	}
	return rows, int(math.Round(globalMin)), int(math.Round(globalMax))
}

func calcPrecipAccum(data *ForecastResponse) ([]PrecipEntry, string) {
	loc, err := time.LoadLocation(data.Timezone)
	if err != nil {
		loc = time.UTC
	}

	var total float64
	var maxDaily float64
	for _, v := range data.Daily.PrecipSum {
		total += v
		if v > maxDaily {
			maxDaily = v
		}
	}

	var entries []PrecipEntry
	var cumulative float64
	for i, dateStr := range data.Daily.Time {
		t, _ := time.ParseInLocation("2006-01-02", dateStr, loc)
		daily := data.Daily.PrecipSum[i]
		cumulative += daily
		barHeight := 0.0
		if maxDaily > 0 {
			barHeight = (daily / maxDaily) * 20
		}
		entries = append(entries, PrecipEntry{
			Day:           t.Format("Mon"),
			Daily:         fmt.Sprintf("%.2f\"", daily),
			DailyVal:      daily,
			Cumulative:    fmt.Sprintf("%.2f\"", cumulative),
			CumulativeRaw: cumulative,
			BarHeight:     barHeight,
		})
	}
	return entries, fmt.Sprintf("%.2f\"", total)
}

// NWS transformation functions

func (s *server) transformNWSHourly(periods []NWSPeriod, timezone string) []HourlyRow {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	now := s.now().In(loc)

	var rows []HourlyRow
	for _, p := range periods {
		t, err := time.Parse(time.RFC3339, p.StartTime)
		if err != nil {
			continue
		}
		t = t.In(loc)
		if t.Before(now.Truncate(time.Hour)) {
			continue
		}
		if len(rows) >= 12 {
			break
		}

		windSpeed := parseWindSpeed(p.WindSpeed)
		precip := "0%"
		if p.ProbabilityOfPrecipitation.Value != nil {
			precip = fmt.Sprintf("%d%%", *p.ProbabilityOfPrecipitation.Value)
		}
		rows = append(rows, HourlyRow{
			Time:      t.Format("3 PM"),
			Temp:      p.Temperature,
			FeelsLike: windchill(float64(p.Temperature), windSpeed),
			Precip:    precip,
			PrecipRaw: -1, // NWS hourly uses probability, not amount
			WindSpeed: int(math.Round(windSpeed)),
			WindDir:   p.WindDirection,
			Condition: p.ShortForecast,
			IsNow:     t.Hour() == now.Hour(),
		})
	}
	return rows
}

func (s *server) transformNWSDaily(periods []NWSPeriod, timezone string) ([]DailyRow, int, int) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	today := s.now().In(loc).Format("2006-01-02")

	// Group periods by date, extracting high (daytime) and low (nighttime)
	type dayData struct {
		date      string
		high      int
		low       int
		wind      int
		precip    int // max probability of precipitation
		condition string
		hasHigh   bool
		hasLow    bool
	}
	days := make(map[string]*dayData)
	var dateOrder []string

	for _, p := range periods {
		t, err := time.Parse(time.RFC3339, p.StartTime)
		if err != nil {
			continue
		}
		dateStr := t.In(loc).Format("2006-01-02")

		if _, exists := days[dateStr]; !exists {
			days[dateStr] = &dayData{date: dateStr}
			dateOrder = append(dateOrder, dateStr)
		}

		d := days[dateStr]
		windSpeed := int(parseWindSpeed(p.WindSpeed))
		if windSpeed > d.wind {
			d.wind = windSpeed
		}

		// Track max precipitation probability for the day
		if p.ProbabilityOfPrecipitation.Value != nil && *p.ProbabilityOfPrecipitation.Value > d.precip {
			d.precip = *p.ProbabilityOfPrecipitation.Value
		}

		if p.IsDaytime {
			d.high = p.Temperature
			d.condition = p.ShortForecast
			d.hasHigh = true
		} else {
			d.low = p.Temperature
			d.hasLow = true
			if !d.hasHigh {
				d.condition = p.ShortForecast
			}
		}
	}

	// Find global min/max for bar scaling
	globalMin := math.Inf(1)
	globalMax := math.Inf(-1)
	for _, d := range days {
		if d.hasLow && float64(d.low) < globalMin {
			globalMin = float64(d.low)
		}
		if d.hasHigh && float64(d.high) > globalMax {
			globalMax = float64(d.high)
		}
	}
	tempRange := globalMax - globalMin
	if tempRange == 0 {
		tempRange = 1
	}

	var rows []DailyRow
	for _, dateStr := range dateOrder {
		d := days[dateStr]
		if !d.hasHigh && !d.hasLow {
			continue
		}

		t, _ := time.ParseInLocation("2006-01-02", dateStr, loc)
		low := d.low
		high := d.high
		if !d.hasLow {
			low = high - 10 // estimate
		}
		if !d.hasHigh {
			high = low + 10 // estimate
		}

		barLeft := ((float64(low) - globalMin) / tempRange) * 100
		barWidth := ((float64(high) - float64(low)) / tempRange) * 100
		if barWidth < 2 {
			barWidth = 2
		}

		rows = append(rows, DailyRow{
			Date:      t.Format("Mon 01/02"),
			DateISO:   dateStr,
			Low:       low,
			High:      high,
			LowColor:  tempToColor(float64(low)),
			HighColor: tempToColor(float64(high)),
			BarLeft:   math.Round(barLeft*10) / 10,
			BarWidth:  math.Round(barWidth*10) / 10,
			Precip:    fmt.Sprintf("%d%%", d.precip),
			PrecipRaw: -1, // Will be updated with QPF data if available
			Wind:      d.wind,
			Condition: d.condition,
			IsToday:   dateStr == today,
		})

		if len(rows) >= 7 {
			break
		}
	}

	return rows, int(math.Round(globalMin)), int(math.Round(globalMax))
}

// parseISO8601Duration parses durations like "PT6H" and returns hours
func parseISO8601Duration(dur string) int {
	// Match patterns like PT6H, PT1H, PT12H
	re := regexp.MustCompile(`PT(\d+)H`)
	matches := re.FindStringSubmatch(dur)
	if len(matches) == 2 {
		hours, _ := strconv.Atoi(matches[1])
		return hours
	}
	return 6 // default to 6 hours
}

// aggregateNWSPrecipByDay takes gridpoint QPF data and returns daily totals in inches
// The dates parameter provides the ordered list of dates to aggregate for
func aggregateNWSPrecipByDay(gridData *NWSGridpointResponse, dates []string, timezone string) map[string]float64 {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	dailyPrecip := make(map[string]float64)
	for _, dateStr := range dates {
		dailyPrecip[dateStr] = 0
	}

	for _, v := range gridData.Properties.QuantitativePrecipitation.Values {
		if v.Value == nil {
			continue
		}

		// Parse validTime like "2026-02-04T12:00:00+00:00/PT6H"
		parts := strings.Split(v.ValidTime, "/")
		if len(parts) != 2 {
			continue
		}

		startTime, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}
		startTime = startTime.In(loc)
		hours := parseISO8601Duration(parts[1])

		// Distribute precipitation across the hours, attributing to the start day
		dateStr := startTime.Format("2006-01-02")
		if _, exists := dailyPrecip[dateStr]; exists {
			// Convert mm to inches
			precipInches := *v.Value / 25.4
			dailyPrecip[dateStr] += precipInches
		}

		// If the period spans midnight, also add to the next day
		endTime := startTime.Add(time.Duration(hours) * time.Hour)
		if endTime.Day() != startTime.Day() {
			nextDateStr := endTime.Format("2006-01-02")
			if _, exists := dailyPrecip[nextDateStr]; exists {
				// Rough approximation: attribute based on hours in each day
				hoursInFirst := 24 - startTime.Hour()
				hoursInSecond := hours - hoursInFirst
				if hoursInSecond > 0 && hours > 0 {
					precipInches := *v.Value / 25.4
					// Redistribute: first day gets its portion, second day gets rest
					dailyPrecip[dateStr] = dailyPrecip[dateStr] - precipInches + (precipInches * float64(hoursInFirst) / float64(hours))
					dailyPrecip[nextDateStr] += precipInches * float64(hoursInSecond) / float64(hours)
				}
			}
		}
	}

	return dailyPrecip
}

// distributeQPFToHours takes gridpoint QPF data and returns hourly precip in inches for a specific date
func distributeQPFToHours(gridData *NWSGridpointResponse, targetDate string, timezone string) map[int]float64 {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	hourlyPrecip := make(map[int]float64)

	for _, v := range gridData.Properties.QuantitativePrecipitation.Values {
		if v.Value == nil || *v.Value == 0 {
			continue
		}

		parts := strings.Split(v.ValidTime, "/")
		if len(parts) != 2 {
			continue
		}

		startTime, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}
		startTime = startTime.In(loc)
		hours := parseISO8601Duration(parts[1])
		if hours <= 0 {
			hours = 1
		}

		precipPerHour := (*v.Value / 25.4) / float64(hours) // mm to inches, distributed

		for h := 0; h < hours; h++ {
			t := startTime.Add(time.Duration(h) * time.Hour)
			if t.Format("2006-01-02") == targetDate {
				hourlyPrecip[t.Hour()] += precipPerHour
			}
		}
	}

	return hourlyPrecip
}

func (s *server) calcNWSPrecipWithQPF(days []DailyRow, precipByDay map[string]float64, timezone string) ([]PrecipEntry, string) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	var entries []PrecipEntry
	var total float64
	var maxDaily float64

	// First pass to find max for bar scaling
	for _, d := range days {
		// Parse date from "Mon 01/02" format - need to figure out the year
		t, err := time.ParseInLocation("Mon 01/02", d.Date, loc)
		if err != nil {
			continue
		}
		// Set year to current year
		now := s.now().In(loc)
		t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		dateStr := t.Format("2006-01-02")

		precip := precipByDay[dateStr]
		if precip > maxDaily {
			maxDaily = precip
		}
		total += precip
	}

	// Second pass to build entries
	var cumulative float64
	for _, d := range days {
		t, err := time.ParseInLocation("Mon 01/02", d.Date, loc)
		if err != nil {
			continue
		}
		now := s.now().In(loc)
		t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		dateStr := t.Format("2006-01-02")

		precip := precipByDay[dateStr]
		cumulative += precip

		barHeight := 0.0
		if maxDaily > 0 {
			barHeight = (precip / maxDaily) * 20
		}

		entries = append(entries, PrecipEntry{
			Day:           d.Date[:3],
			Daily:         fmt.Sprintf("%.2f\"", precip),
			DailyVal:      precip,
			Cumulative:    fmt.Sprintf("%.2f\"", cumulative),
			CumulativeRaw: cumulative,
			BarHeight:     barHeight,
		})
	}

	return entries, fmt.Sprintf("%.2f\"", total)
}

func calcNWSPrecip(days []DailyRow) ([]PrecipEntry, string) {
	var entries []PrecipEntry
	var maxPrecip int
	for _, d := range days {
		// Extract percentage value from string like "40%"
		var pct int
		fmt.Sscanf(d.Precip, "%d%%", &pct)
		if pct > maxPrecip {
			maxPrecip = pct
		}
	}

	for _, d := range days {
		dayName := d.Date[:3]
		var pct int
		fmt.Sscanf(d.Precip, "%d%%", &pct)
		barHeight := 0.0
		if maxPrecip > 0 {
			barHeight = (float64(pct) / float64(maxPrecip)) * 20
		}
		entries = append(entries, PrecipEntry{
			Day:           dayName,
			Daily:         d.Precip, // Shows as "40%" etc
			DailyVal:      -1,       // Probability, not amount
			Cumulative:    "--",     // Accumulation doesn't make sense for probability
			CumulativeRaw: -1,
			BarHeight:     barHeight,
		})
	}
	return entries, fmt.Sprintf("%d%% max", maxPrecip)
}

// transformHourlyForDay filters Open-Meteo hourly data to a specific date
func (s *server) transformHourlyForDay(data *ForecastResponse, targetDate string) []HourlyRow {
	loc, err := time.LoadLocation(data.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := s.now().In(loc)
	today := now.Format("2006-01-02")

	var rows []HourlyRow
	for i, ts := range data.Hourly.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if err != nil {
			continue
		}
		dateStr := t.Format("2006-01-02")
		if dateStr != targetDate {
			continue
		}
		// Skip past hours for today
		if dateStr == today && t.Before(now.Truncate(time.Hour)) {
			continue
		}
		rows = append(rows, HourlyRow{
			Time:      t.Format("3 PM"),
			Temp:      int(math.Round(data.Hourly.Temperature[i])),
			FeelsLike: int(math.Round(data.Hourly.ApparentTemp[i])),
			Precip:    fmt.Sprintf("%.2f\"", data.Hourly.Precipitation[i]),
			PrecipRaw: data.Hourly.Precipitation[i],
			WindSpeed: int(math.Round(data.Hourly.WindSpeed[i])),
			WindDir:   windDirLabel(data.Hourly.WindDirection[i]),
			Condition: wmoDescription(data.Hourly.WeatherCode[i]),
			IsNow:     dateStr == today && t.Hour() == now.Hour(),
		})
	}
	return rows
}

// transformNWSHourlyForDay filters NWS hourly periods to a specific date
func (s *server) transformNWSHourlyForDay(periods []NWSPeriod, timezone string, targetDate string) []HourlyRow {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	now := s.now().In(loc)
	today := now.Format("2006-01-02")

	var rows []HourlyRow
	for _, p := range periods {
		t, err := time.Parse(time.RFC3339, p.StartTime)
		if err != nil {
			continue
		}
		t = t.In(loc)
		dateStr := t.Format("2006-01-02")
		if dateStr != targetDate {
			continue
		}
		// Skip past hours for today
		if dateStr == today && t.Before(now.Truncate(time.Hour)) {
			continue
		}

		windSpeed := parseWindSpeed(p.WindSpeed)
		precip := "0%"
		if p.ProbabilityOfPrecipitation.Value != nil {
			precip = fmt.Sprintf("%d%%", *p.ProbabilityOfPrecipitation.Value)
		}
		rows = append(rows, HourlyRow{
			Time:      t.Format("3 PM"),
			Temp:      p.Temperature,
			FeelsLike: windchill(float64(p.Temperature), windSpeed),
			Precip:    precip,
			PrecipRaw: -1, // NWS hourly uses probability, not amount
			WindSpeed: int(math.Round(windSpeed)),
			WindDir:   p.WindDirection,
			Condition: p.ShortForecast,
			IsNow:     dateStr == today && t.Hour() == now.Hour(),
		})
	}
	return rows
}

// getDailyRowForDate finds the DailyRow for a specific date
func getDailyRowForDate(days []DailyRow, targetDate string) *DailyRow {
	for i := range days {
		if days[i].DateISO == targetDate {
			return &days[i]
		}
	}
	return nil
}

// getAdjacentDates returns prev and next day ISO dates based on available days
func getAdjacentDates(days []DailyRow, targetDate string) (prev, next string) {
	for i, d := range days {
		if d.DateISO == targetDate {
			if i > 0 {
				prev = days[i-1].DateISO
			}
			if i < len(days)-1 {
				next = days[i+1].DateISO
			}
			return
		}
	}
	return
}

func (s *server) handleDayForecast(w http.ResponseWriter, r *http.Request) {
	lat := r.URL.Query().Get("lat")
	lon := r.URL.Query().Get("lon")
	name := r.URL.Query().Get("name")
	dayParam := r.URL.Query().Get("day")

	if lat == "" || lon == "" || dayParam == "" {
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Missing parameters"})
		return
	}

	// Parse the target date
	targetDate, err := time.Parse("2006-01-02", dayParam)
	if err != nil {
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Invalid date format"})
		return
	}

	var dfd DayForecastData
	dfd.Lat = lat
	dfd.Lon = lon
	dfd.Name = name
	dfd.DateISO = dayParam
	dfd.Date = targetDate.Format("Monday, January 2")
	dfd.DateShort = targetDate.Format("Mon 01/02")

	// Try NWS API first (for US locations)
	nwsPoints, err := s.fetchNWSPoints(lat, lon)
	if err == nil {
		// NWS succeeded - this is a US location
		log.Printf("Using NWS API for day forecast %s,%s day=%s", lat, lon, dayParam)

		// Fetch daily and hourly forecasts
		daily, err := fetchNWSForecast(nwsPoints.Properties.Forecast)
		if err != nil {
			log.Printf("NWS daily forecast error: %v, falling back to Open-Meteo", err)
			goto openMeteoDayForecast
		}
		hourly, err := fetchNWSForecast(nwsPoints.Properties.ForecastHourly)
		if err != nil {
			log.Printf("NWS hourly forecast error: %v, falling back to Open-Meteo", err)
			goto openMeteoDayForecast
		}

		timezone := nwsPoints.Properties.TimeZone
		if timezone == "" {
			timezone = "America/New_York"
		}
		dfd.Timezone = timezone

		// Get daily rows for summary and navigation
		days, _, _ := s.transformNWSDaily(daily.Properties.Periods, timezone)

		// Update precip with QPF if available
		gridpoint, err := s.fetchNWSGridpoint(nwsPoints.Properties.GridId, nwsPoints.Properties.GridX, nwsPoints.Properties.GridY)
		if err == nil {
			loc, _ := time.LoadLocation(timezone)
			if loc == nil {
				loc = time.UTC
			}
			now := s.now().In(loc)
			var dates []string
			for _, d := range days {
				t, err := time.ParseInLocation("Mon 01/02", d.Date, loc)
				if err == nil {
					t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
					dates = append(dates, t.Format("2006-01-02"))
				}
			}
			precipByDay := aggregateNWSPrecipByDay(gridpoint, dates, timezone)
			for i := range days {
				if i < len(dates) {
					days[i].Precip = fmt.Sprintf("%.2f\"", precipByDay[dates[i]])
					days[i].PrecipRaw = precipByDay[dates[i]]
				}
			}
		}

		// Find the summary for the target day
		summary := getDailyRowForDate(days, dayParam)
		if summary == nil {
			s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Day not found in forecast"})
			return
		}
		dfd.Summary = *summary

		// Get hourly data for this day
		dfd.Hours = s.transformNWSHourlyForDay(hourly.Properties.Periods, timezone, dayParam)

		// Overlay QPF precipitation amounts onto hourly rows
		if gridpoint != nil {
			hourlyQPF := distributeQPFToHours(gridpoint, dayParam, timezone)
			for i := range dfd.Hours {
				t, _ := time.ParseInLocation("3 PM", dfd.Hours[i].Time, time.UTC)
				precip := hourlyQPF[t.Hour()]
				dfd.Hours[i].PrecipRaw = precip
				dfd.Hours[i].Precip = fmt.Sprintf("%.2f\"", precip)
			}
		}

		// Get adjacent days for navigation
		dfd.PrevDay, dfd.NextDay = getAdjacentDates(days, dayParam)

		if name == "" {
			name = fmt.Sprintf("%.4s, %.4s", lat, lon)
			dfd.Name = name
		}

		s.templates.ExecuteTemplate(w, "day.html", dfd)
		return
	}

	log.Printf("NWS points error for day forecast %s,%s: %v, using Open-Meteo", lat, lon, err)

openMeteoDayForecast:
	// Fallback to Open-Meteo
	data, err := s.fetchForecast(lat, lon)
	if err != nil {
		log.Printf("Open-Meteo forecast error: %v", err)
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Weather data unavailable"})
		return
	}

	dfd.Timezone = data.Timezone

	// Get daily rows for summary and navigation
	days, _, _ := s.transformDaily(data)

	// Find the summary for the target day
	summary := getDailyRowForDate(days, dayParam)
	if summary == nil {
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Day not found in forecast"})
		return
	}
	dfd.Summary = *summary

	// Get hourly data for this day
	dfd.Hours = s.transformHourlyForDay(data, dayParam)

	// Get adjacent days for navigation
	dfd.PrevDay, dfd.NextDay = getAdjacentDates(days, dayParam)

	if name == "" {
		name = fmt.Sprintf("%.2f, %.2f", data.Latitude, data.Longitude)
		dfd.Name = name
	}

	s.templates.ExecuteTemplate(w, "day.html", dfd)
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.templates.ExecuteTemplate(w, "base.html", BaseData{
		DevMode: os.Getenv("DEV") == "1",
	})
}

func (s *server) handleForecast(w http.ResponseWriter, r *http.Request) {
	lat := r.URL.Query().Get("lat")
	lon := r.URL.Query().Get("lon")
	name := r.URL.Query().Get("name")

	if lat == "" || lon == "" {
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Missing coordinates"})
		return
	}

	var fd ForecastData
	fd.Lat = lat
	fd.Lon = lon
	fd.Name = name

	// Try NWS API first (for US locations)
	nwsPoints, err := s.fetchNWSPoints(lat, lon)
	if err == nil {
		// NWS succeeded - this is a US location
		log.Printf("Using NWS API for %s,%s", lat, lon)

		// Fetch daily and hourly forecasts
		daily, err := fetchNWSForecast(nwsPoints.Properties.Forecast)
		if err != nil {
			log.Printf("NWS daily forecast error: %v, falling back to Open-Meteo", err)
			goto openMeteo
		}
		hourly, err := fetchNWSForecast(nwsPoints.Properties.ForecastHourly)
		if err != nil {
			log.Printf("NWS hourly forecast error: %v, falling back to Open-Meteo", err)
			goto openMeteo
		}

		timezone := nwsPoints.Properties.TimeZone
		if timezone == "" {
			timezone = "America/New_York"
		}

		allHours := s.transformNWSHourly(hourly.Properties.Periods, timezone)
		days, globalLow, globalHigh := s.transformNWSDaily(daily.Properties.Periods, timezone)

		// Fetch gridpoint data for quantitative precipitation
		var precip []PrecipEntry
		var totalPrecip string
		gridpoint, err := s.fetchNWSGridpoint(nwsPoints.Properties.GridId, nwsPoints.Properties.GridX, nwsPoints.Properties.GridY)
		if err == nil {
			// Extract dates from days for aggregation
			loc, _ := time.LoadLocation(timezone)
			if loc == nil {
				loc = time.UTC
			}
			now := s.now().In(loc)
			var dates []string
			for _, d := range days {
				// Parse "Mon 01/02" format
				t, err := time.ParseInLocation("Mon 01/02", d.Date, loc)
				if err == nil {
					t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
					dates = append(dates, t.Format("2006-01-02"))
				}
			}
			precipByDay := aggregateNWSPrecipByDay(gridpoint, dates, timezone)
			precip, totalPrecip = s.calcNWSPrecipWithQPF(days, precipByDay, timezone)

			// Also update the daily rows with actual precip values
			for i := range days {
				if i < len(dates) {
					days[i].Precip = fmt.Sprintf("%.2f\"", precipByDay[dates[i]])
					days[i].PrecipRaw = precipByDay[dates[i]]
				}
			}

			// Also update the hourly rows with QPF precipitation amounts
			todayStr := now.Format("2006-01-02")
			tomorrowStr := now.Add(24 * time.Hour).Format("2006-01-02")
			todayQPF := distributeQPFToHours(gridpoint, todayStr, timezone)
			tomorrowQPF := distributeQPFToHours(gridpoint, tomorrowStr, timezone)
			hourCursor := now.Truncate(time.Hour)
			for i := range allHours {
				t := hourCursor.Add(time.Duration(i) * time.Hour)
				dateStr := t.Format("2006-01-02")
				hour := t.Hour()
				var precipVal float64
				if dateStr == todayStr {
					precipVal = todayQPF[hour]
				} else {
					precipVal = tomorrowQPF[hour]
				}
				allHours[i].PrecipRaw = precipVal
				allHours[i].Precip = fmt.Sprintf("%.2f\"", precipVal)
			}
		} else {
			log.Printf("NWS gridpoint error: %v, using probability only", err)
			precip, totalPrecip = calcNWSPrecip(days)
		}

		if name == "" {
			name = fmt.Sprintf("%.4s, %.4s", lat, lon)
		}

		const visibleHours = 3
		hours := allHours
		var moreHours []HourlyRow
		if len(allHours) > visibleHours {
			hours = allHours[:visibleHours]
			moreHours = allHours[visibleHours:]
		}

		// Calculate raw total from last cumulative entry
		totalPrecipRaw := -1.0
		if len(precip) > 0 && precip[len(precip)-1].CumulativeRaw >= 0 {
			totalPrecipRaw = precip[len(precip)-1].CumulativeRaw
		}

		fd = ForecastData{
			Name:           name,
			Lat:            lat,
			Lon:            lon,
			Timezone:       timezone,
			Elevation:      0, // NWS doesn't provide elevation in points response
			Hours:          hours,
			MoreHours:      moreHours,
			MoreCount:      len(moreHours),
			Days:           days,
			Precip:         precip,
			TotalPrecip:    totalPrecip,
			TotalPrecipRaw: totalPrecipRaw,
			GlobalLow:      globalLow,
			GlobalHigh:     globalHigh,
		}

		s.templates.ExecuteTemplate(w, "forecast.html", fd)
		return
	}

	log.Printf("NWS points error for %s,%s: %v, using Open-Meteo", lat, lon, err)

openMeteo:
	// Fallback to Open-Meteo (for non-US or NWS failures)
	data, err := s.fetchForecast(lat, lon)
	if err != nil {
		log.Printf("Open-Meteo forecast error: %v", err)
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Weather data unavailable"})
		return
	}

	allHours := s.transformHourly(data)
	days, globalLow, globalHigh := s.transformDaily(data)
	precip, totalPrecip := calcPrecipAccum(data)

	if name == "" {
		name = fmt.Sprintf("%.2f, %.2f", data.Latitude, data.Longitude)
	}

	const visibleHours = 3
	hours := allHours
	var moreHours []HourlyRow
	if len(allHours) > visibleHours {
		hours = allHours[:visibleHours]
		moreHours = allHours[visibleHours:]
	}

	// Calculate raw total from last cumulative entry
	totalPrecipRaw := -1.0
	if len(precip) > 0 && precip[len(precip)-1].CumulativeRaw >= 0 {
		totalPrecipRaw = precip[len(precip)-1].CumulativeRaw
	}

	fd = ForecastData{
		Name:           name,
		Lat:            lat,
		Lon:            lon,
		Timezone:       data.Timezone,
		Elevation:      int(math.Round(data.Elevation * 3.28084)), // meters to feet
		Hours:          hours,
		MoreHours:      moreHours,
		MoreCount:      len(moreHours),
		Days:           days,
		Precip:         precip,
		TotalPrecip:    totalPrecip,
		TotalPrecipRaw: totalPrecipRaw,
		GlobalLow:      globalLow,
		GlobalHigh:     globalHigh,
	}

	s.templates.ExecuteTemplate(w, "forecast.html", fd)
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		w.Write([]byte(""))
		return
	}

	data, err := s.fetchGeocode(q)
	if err != nil || len(data.Results) == 0 {
		w.Write([]byte(`<div class="search-empty">No results</div>`))
		return
	}

	s.templates.ExecuteTemplate(w, "search.html", SearchData{
		Results: geocodeToSearchResults(data.Results),
		Query:   q,
	})
}

func (s *server) handleNearby(w http.ResponseWriter, r *http.Request) {
	lat := r.URL.Query().Get("lat")
	lon := r.URL.Query().Get("lon")
	if lat == "" || lon == "" {
		s.templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Missing coordinates"})
		return
	}

	cityName, err := s.reverseGeocode(lat, lon)
	if err != nil || cityName == "" {
		log.Printf("reverse geocode failed: %v", err)
		s.handleForecast(w, r)
		return
	}

	data, err := s.fetchGeocode(cityName)
	if err != nil || len(data.Results) == 0 {
		s.handleForecast(w, r)
		return
	}

	userLat, err1 := strconv.ParseFloat(lat, 64)
	userLon, err2 := strconv.ParseFloat(lon, 64)
	if err1 != nil || err2 != nil {
		s.handleForecast(w, r)
		return
	}

	// Filter to cities within 200km and sort by distance
	type resultWithDist struct {
		result GeocodeResult
		dist   float64
	}
	var nearby []resultWithDist
	for _, r := range data.Results {
		d := haversine(userLat, userLon, r.Latitude, r.Longitude)
		if d <= 200 {
			nearby = append(nearby, resultWithDist{r, d})
		}
	}
	sort.Slice(nearby, func(i, j int) bool { return nearby[i].dist < nearby[j].dist })

	if len(nearby) == 0 {
		s.handleForecast(w, r)
		return
	}

	var filtered []GeocodeResult
	for _, n := range nearby {
		filtered = append(filtered, n.result)
	}

	s.templates.ExecuteTemplate(w, "nearby.html", NearbyData{
		Results: geocodeToSearchResults(filtered),
		Lat:     lat,
		Lon:     lon,
	})
}

func main() {
	funcMap := template.FuncMap{
		"gt": func(a, b float64) bool { return a > b },
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html"))

	s := &server{
		now:              time.Now,
		openMeteoBaseURL: "https://api.open-meteo.com",
		geocodeBaseURL:   "https://geocoding-api.open-meteo.com",
		nwsBaseURL:       "https://api.weather.gov",
		nominatimBaseURL: "https://nominatim.openstreetmap.org",
		templates:        tmpl,
	}

	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/forecast", s.handleForecast)
	http.HandleFunc("/day-forecast", s.handleDayForecast)
	http.HandleFunc("/search", s.handleSearch)
	http.HandleFunc("/nearby", s.handleNearby)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("Weather server starting on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
