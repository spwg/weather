# WX

Minimal, information-dense weather dashboard. Go backend (stdlib only), htmx frontend, dual weather API support (NWS + Open-Meteo).

## Features

- **Hourly forecast** for the current day: temperature, feels like, precipitation, wind speed/direction, conditions
- **7-day daily forecast** with temperature range bars scaled to the global min/max across the forecast window
- **Day detail view** — click any day in the 7-day forecast to see its full 24-hour hourly breakdown with day summary
- **Precipitation accumulation** table showing daily amounts and running cumulative totals
- **Hourly precipitation amounts** for US locations via NWS gridpoint QPF (Quantitative Precipitation Forecast) data
- **Unit preferences** — toggle between °F/°C and in/mm, persisted in localStorage
- **City search** with debounced typeahead and keyboard navigation via Open-Meteo geocoding
- **Nearby cities** picker based on current location
- **Dual API support** — NWS API for US locations (higher accuracy), Open-Meteo for international
- **Browser geolocation** auto-detects on load, falls back to search prompt if denied
- **Live timezone clock** showing the forecast location's local time
- **Current hour/day highlighting** with accent border on active rows
- **URL state** — shareable links with hash params (`#lat=...&lon=...&name=...&day=...`)

## Stack

- **Backend:** Go standard library (`net/http`, `html/template`, `encoding/json`) — zero external dependencies
- **Frontend:** [htmx](https://htmx.org/) for HTML fragment swapping, pure CSS dark theme
- **APIs:**
  - [NWS API](https://www.weather.gov/documentation/services-web-api) — US locations (forecast, hourly, gridpoint QPF)
  - [Open-Meteo](https://open-meteo.com/) — international locations and geocoding, no API key required

## Live

**https://wx-weather.fly.dev**

## Running locally

```
go run main.go
```

Open http://localhost:8080

## Development

For auto-rebuild and browser refresh on file changes, install [livereload](https://github.com/spwg/livereload):

```
go install github.com/spwg/livereload@latest
```

Then run in a separate terminal:

```
livereload
```

This runs continuously, watching for changes to `.go` files and templates. On each change it rebuilds the server and triggers a browser reload. The `DEV=1` environment variable is set automatically, which enables the livereload script in the HTML.

## Deploying

Hosted on [Fly.io](https://fly.io/) using the included `Dockerfile` and `fly.toml`.

```
fly auth login
fly deploy
```

The app reads `PORT` from the environment (defaults to 8080). Fly machines auto-stop when idle and wake on incoming requests.

## How it works

The server exposes these routes:

| Route | Purpose |
|---|---|
| `GET /` | Serves the page shell with htmx and geolocation JS |
| `GET /forecast?lat=&lon=&name=` | Fetches weather data, returns the main forecast HTML fragment |
| `GET /day-forecast?lat=&lon=&day=&name=` | Fetches a single day's hourly detail, returns day view HTML fragment |
| `GET /search?q=` | Geocodes a location name, returns clickable results as an HTML fragment |
| `GET /nearby?lat=&lon=` | Finds nearby cities, returns a picker HTML fragment |

For US locations, the server tries the NWS API first (`/points`, `/forecast`, `/forecastHourly`, `/gridpoints` for QPF data). If NWS fails or the location is outside the US, it falls back to Open-Meteo.

On page load, the browser geolocation API requests coordinates. On success, htmx fires a request to `/forecast` and swaps the result into the page. If geolocation is denied, the user types a city into the search box, which triggers debounced requests to `/search`. Clicking a result loads that city's forecast.

All state is managed via URL hash parameters, so bookmarks and browser back/forward work as expected. Unit preferences (temperature and precipitation) are stored in localStorage and applied client-side.

All data transforms (zipping parallel arrays, computing temperature bar percentages, accumulating precipitation, distributing QPF to hourly buckets) happen server-side in `main.go`. Templates receive ready-to-render view-model structs.

## Project structure

```
main.go              Server, routes, API calls, data transforms
go.mod               Module file (no external deps)
livereload.toml      Dev server config (auto-rebuild + browser refresh)
Dockerfile           Multi-stage build (compile + alpine runtime)
fly.toml             Fly.io deployment config
templates/
  base.html          Page shell: htmx script, search input, geolocation JS, unit toggles
  forecast.html      Hourly table + daily forecast with range bars + precipitation
  day.html           Day detail view: day summary + full 24-hour hourly table
  search.html        Geocoding results dropdown
  nearby.html        Nearby cities picker
  error.html         Error message fragment
static/
  style.css          Dark theme, monospace typography, range bar CSS
```
