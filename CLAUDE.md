# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
go run main.go              # start server on :8080
go test ./...               # run all tests (89 tests, ~95% coverage)
go test -run TestFuncName   # run a single test
livereload                  # dev server with auto-rebuild + browser refresh (needs go install github.com/spwg/livereload@latest)
fly deploy                  # deploy to Fly.io (push to github to trigger deploy)
```

## Formatting

Run `gofmt -w .` to format the code before committing.

## Architecture

Single-file Go app (`main.go`) with zero external dependencies. All application code lives in one file; templates in `templates/`.

### Server struct & dependency injection

The `server` struct holds injectable deps: `now func() time.Time`, four `*BaseURL` string fields, and `templates`. Methods on `*server` (receiver name `s`) handle anything needing time, URLs, or templates. Pure utility functions (`wmoDescription`, `windDirLabel`, `tempToColor`, etc.) remain standalone with no receiver.

### Dual API with fallback

Forecast handlers try NWS first for US locations (`fetchNWSPoints`), then fall back to Open-Meteo via `goto` on any NWS failure. Open-Meteo works globally. NWS provides QPF (quantitative precipitation) data when gridpoint fetch succeeds; without it, only probability percentages are shown.

### Data flow

Raw API responses → transform functions (`transformHourly`, `transformDaily`, `calcPrecipAccum`, etc.) → view-model structs → HTML templates. Each API (NWS, Open-Meteo) has its own transform variant. Precipitation has three modes: Open-Meteo inches (`PrecipRaw >= 0`), NWS QPF inches, or NWS probability (`PrecipRaw == -1`).

### Timezone handling

All timezone-aware code follows the pattern: `loc, err := time.LoadLocation(timezone)` then `now := s.now().In(loc)`. The `time/tzdata` blank import embeds the IANA timezone database in the binary — **do not remove this import**. Alpine Linux (used in the Docker runtime) has no system tzdata; without the embedded DB, `LoadLocation` silently fails to UTC, breaking hour highlighting and local clock display. This bug only manifests in production, not locally.

## Testing

- `newTestServer(t, opts ...func(*server))` — creates server with fixed clock (`2026-02-08 15:00 UTC`) and real templates
- `setupAllMockServer(t)` — returns server with all four APIs mocked (NWS, Open-Meteo, geocoding, Nominatim)
- Override individual base URLs via option funcs: `newTestServer(t, func(s *server) { s.nwsBaseURL = mockURL })`
- Pure function tests need no server instance
- Templates loaded from `templates/*.html` — tests require actual template files on disk

## Style

- Google Go Style Guide conventions (https://google.github.io/styleguide/go/, https://google.github.io/styleguide/go/guide, https://google.github.io/styleguide/go/decisions, https://google.github.io/styleguide/go/best-practices)
- Receiver name always `s` for `*server`
- No premature interfaces
