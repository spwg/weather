package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"net/url"
	"sort"
	"strconv"
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

// View-model structs for templates

type HourlyRow struct {
	Time      string
	Temp      int
	FeelsLike int
	Precip    string
	WindSpeed int
	WindDir   string
	Condition string
	IsNow     bool
}

type DailyRow struct {
	Date      string
	Low       int
	High      int
	LowColor  string
	HighColor string
	BarLeft   float64
	BarWidth  float64
	Precip    string
	Wind      int
	Condition string
	IsToday   bool
}

type PrecipEntry struct {
	Day        string
	Daily      string
	DailyVal   float64
	Cumulative string
	BarHeight  float64
}

type ForecastData struct {
	Name        string
	Lat         string
	Lon         string
	Timezone    string
	Elevation   int
	Hours       []HourlyRow
	MoreHours   []HourlyRow
	MoreCount   int
	Days        []DailyRow
	Precip      []PrecipEntry
	TotalPrecip string
	GlobalLow   int
	GlobalHigh  int
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

var templates *template.Template

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

func fetchForecast(lat, lon string) (*ForecastResponse, error) {
	apiURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s"+
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

func fetchGeocode(query string) (*GeocodeResponse, error) {
	apiURL := fmt.Sprintf(
		"https://geocoding-api.open-meteo.com/v1/search?name=%s&count=5&language=en&format=json",
		url.QueryEscape(query),
	)
	var data GeocodeResponse
	if err := fetchJSON(apiURL, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func reverseGeocode(lat, lon string) (string, error) {
	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?lat=%s&lon=%s&format=json&zoom=10",
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

func transformHourly(data *ForecastResponse) []HourlyRow {
	loc, err := time.LoadLocation(data.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

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
			Precip:    fmt.Sprintf("%.2f", data.Hourly.Precipitation[i]),
			WindSpeed: int(math.Round(data.Hourly.WindSpeed[i])),
			WindDir:   windDirLabel(data.Hourly.WindDirection[i]),
			Condition: wmoDescription(data.Hourly.WeatherCode[i]),
			IsNow:     t.Hour() == now.Hour(),
		})
	}
	return rows
}

func transformDaily(data *ForecastResponse) ([]DailyRow, int, int) {
	loc, err := time.LoadLocation(data.Timezone)
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc).Format("2006-01-02")

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
			Low:       int(math.Round(low)),
			High:      int(math.Round(high)),
			LowColor:  tempToColor(low),
			HighColor: tempToColor(high),
			BarLeft:   math.Round(barLeft*10) / 10,
			BarWidth:  math.Round(barWidth*10) / 10,
			Precip:    fmt.Sprintf("%.2f", data.Daily.PrecipSum[i]),
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
			Day:        t.Format("Mon"),
			Daily:      fmt.Sprintf("%.2f", daily),
			DailyVal:   daily,
			Cumulative: fmt.Sprintf("%.2f", cumulative),
			BarHeight:  barHeight,
		})
	}
	return entries, fmt.Sprintf("%.2f", total)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	templates.ExecuteTemplate(w, "base.html", nil)
}

func handleForecast(w http.ResponseWriter, r *http.Request) {
	lat := r.URL.Query().Get("lat")
	lon := r.URL.Query().Get("lon")
	name := r.URL.Query().Get("name")

	if lat == "" || lon == "" {
		templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Missing coordinates"})
		return
	}

	data, err := fetchForecast(lat, lon)
	if err != nil {
		log.Printf("forecast error: %v", err)
		templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Weather data unavailable"})
		return
	}

	allHours := transformHourly(data)
	days, globalLow, globalHigh := transformDaily(data)
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

	fd := ForecastData{
		Name:        name,
		Lat:         lat,
		Lon:         lon,
		Timezone:    data.Timezone,
		Elevation:   int(math.Round(data.Elevation * 3.28084)), // meters to feet
		Hours:       hours,
		MoreHours:   moreHours,
		MoreCount:   len(moreHours),
		Days:        days,
		Precip:      precip,
		TotalPrecip: totalPrecip,
		GlobalLow:   globalLow,
		GlobalHigh:  globalHigh,
	}

	templates.ExecuteTemplate(w, "forecast.html", fd)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		w.Write([]byte(""))
		return
	}

	data, err := fetchGeocode(q)
	if err != nil || len(data.Results) == 0 {
		w.Write([]byte(`<div class="search-empty">No results</div>`))
		return
	}

	templates.ExecuteTemplate(w, "search.html", SearchData{
		Results: geocodeToSearchResults(data.Results),
		Query:   q,
	})
}

func handleNearby(w http.ResponseWriter, r *http.Request) {
	lat := r.URL.Query().Get("lat")
	lon := r.URL.Query().Get("lon")
	if lat == "" || lon == "" {
		templates.ExecuteTemplate(w, "error.html", ErrorData{Message: "Missing coordinates"})
		return
	}

	cityName, err := reverseGeocode(lat, lon)
	if err != nil || cityName == "" {
		log.Printf("reverse geocode failed: %v", err)
		handleForecast(w, r)
		return
	}

	data, err := fetchGeocode(cityName)
	if err != nil || len(data.Results) == 0 {
		handleForecast(w, r)
		return
	}

	userLat, err1 := strconv.ParseFloat(lat, 64)
	userLon, err2 := strconv.ParseFloat(lon, 64)
	if err1 != nil || err2 != nil {
		handleForecast(w, r)
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
		handleForecast(w, r)
		return
	}

	var filtered []GeocodeResult
	for _, n := range nearby {
		filtered = append(filtered, n.result)
	}

	templates.ExecuteTemplate(w, "nearby.html", NearbyData{
		Results: geocodeToSearchResults(filtered),
		Lat:     lat,
		Lon:     lon,
	})
}

func main() {
	funcMap := template.FuncMap{
		"gt": func(a, b float64) bool { return a > b },
	}

	templates = template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html"))

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/forecast", handleForecast)
	http.HandleFunc("/search", handleSearch)
	http.HandleFunc("/nearby", handleNearby)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("Weather server starting on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
