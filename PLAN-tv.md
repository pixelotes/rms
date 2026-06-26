# Plan: soporte de TV / IPTV (listas M3U) en RMS

> Objetivo: parsear listas de canales (M3U principal, JSON opcional), agruparlos por
> `group-title`, mostrar nombre (`tvg-name`) y logo (`tvg-logo`), y exponerlos tanto en
> el **WebUI** como en la **API Jellyfin** (incluida la API `/LiveTv/*` que piden los
> clientes). Filosofía del proyecto: **minimum overhead**, sin base de datos.

## Contexto / encaje en la arquitectura actual

El núcleo de RMS es path-based y sin estado: un `idStore` (`sync.Map`) mapea
`ItemID(path)=sha256(path) → path` ([store.go](internal/media/store.go)), y todo se
resuelve por ID a un fichero local que se sirve con `http.ServeFile`
([jf_playback.go](internal/server/jf_playback.go), [stream.go](internal/server/stream.go)).

**El choque conceptual:** un canal de TV no es un fichero local; es una **URL HLS**
(`.m3u8`) remota. No se puede `os.Stat` ni `http.ServeFile`. Además la lista hay que
**parsearla una vez y cachearla** (a diferencia de las librerías de fichero, que se
re-leen del disco en cada request). Por eso el diseño añade un **registro de canales**
en memoria, paralelo al `idStore`, y trata las URLs como fuentes remotas.

El ADR-005 ("No Live TV") contempla justo esto en *Future Considerations*: «If users
request Live TV support, implement it». Este plan **lo revisa** (→ nuevo ADR-015).

---

## Decisiones de diseño

### D1 — Configuración: nuevo `content_type: tv`
```yaml
libraries:
  - friendly_name: "TV"
    content_type: tv
    path: "./lists/tv.m3u"     # fichero .m3u | .json, directorio de listas, o URL http(s)
    # opcionales:
    tv_proxy: false            # true = proxy del stream; false = redirect 302 (default)
    tv_refresh_hours: 0        # re-descargar lista remota cada N horas (0 = solo al boot/rescan)
```
- En [config.go](internal/config/config.go): añadir `TVProxy YAMLBool` y `TVRefreshHours int`
  a `Library`. Relajar `validate()`/`resolvePaths()`: para `content_type: tv` la `path`
  puede ser un **fichero** o una **URL**, no solo un directorio.

### D2 — Parser + ChannelStore (nuevo paquete `internal/tv`)
```go
type Channel struct {
    ID, Name, Group, Logo, URL, TvgID string
    Headers map[string]string // de #EXTVLCOPT (http-user-agent, http-referrer)
    LibIndex, Number int
}
type Group struct { ID, Name string; LibIndex int; ChannelIDs []string }
```
- `ParseM3U(io.Reader)`: lee `#EXTINF:-1 tvg-id=… tvg-logo=… group-title=… tvg-name=…,<display>`,
  acumula líneas `#EXTVLCOPT:` en `Headers`, y toma la línea siguiente no-comentario como `URL`.
- `ParseJSON(io.Reader)`: esquema simple `[{name,group,logo,url,headers}]` (define `lists/tv.json`,
  hoy vacío).
- `ChannelStore` (`sync.Map` id→*Channel + índice de grupos). IDs deterministas:
  `ID = ItemID(URL)`, `Group.ID = stableID("tvgroup|"+libIndex+"|"+group)`.
- `Populate(libs)`: parsea cada librería `tv`, puebla el store. Se invoca desde
  `Server.Start()` y `rescanLibraries()` ([autoscan.go](internal/server/autoscan.go#L124)),
  igual que `PopulateIDStore`. Coste: parsear unos miles de líneas en memoria, despreciable.
- **Dedup opcional**: la lista de ejemplo repite `tvg-name` con varias fuentes; v1 lista
  cada `#EXTINF` como un canal; v3 puede colapsar mismo `tvg-name` en un item con N fuentes.

### D3 — Resolución de IDs
Los IDs de canal/grupo viven en el `ChannelStore`, **no** en el `idStore` de ficheros.
Los handlers que hoy hacen `media.ItemPath(id)` ganan una rama previa:
`if ch, ok := tv.LookupChannel(id); ok { … }`. Afecta a: `jfGetItem`, `jfGetItems`,
`jfPlaybackInfo`, `jfVideoStream`, `jfGetItemImage`, y el browse nativo.

### D4 — Streaming de un canal
- **Redirect (default)**: `http.Redirect(w, r, ch.URL, 302)`. Suficiente para apps Jellyfin
  nativas (reproducen HLS) y para navegadores cuando no hacen falta cabeceras ni hay CORS.
- **Proxy (`tv_proxy: true` o si hay `Headers`)**: RMS hace `GET` con las cabeceras
  `#EXTVLCOPT` y reescribe el manifiesto HLS para que variantes/segmentos vuelvan a pasar
  por `/tv/proxy?u=…`. Es solo *byte-piping* (sin transcodificar) → asumible en una Pi, pero
  cada espectador mantiene una conexión. **Es la parte de más riesgo/esfuerzo.**

### D5 — API Jellyfin LiveTv (revisa ADR-005)
Convertir los stubs de [server.go:333-336](internal/server/server.go#L333) en handlers reales
respaldados por el `ChannelStore`:

| Endpoint | Respuesta |
|---|---|
| `GET /LiveTv/Info` | `{Services:[{Name:"RMS",Status:"Ok",IsVisible:true}], IsEnabled:true}` — **activa la pestaña Live TV** en los clientes |
| `GET /LiveTv/Channels` | `BaseItemDtoQueryResult` de items canal (con `StartIndex/Limit/UserId`, respetando acceso por usuario) |
| `GET /LiveTv/Channels/{id}` | un item canal |
| `GET /LiveTv/Programs*`, `/GuideInfo` | vacío (sin EPG) — se mantienen los stubs |
| `POST /Items/{id}/PlaybackInfo` | rama canal → `MediaSource` HLS |
| `POST /LiveStreams/Open` | `{MediaSource: <misma fuente>}` |
| `POST /LiveStreams/Close` | `204` |

Item canal (`BaseItemDto`):
```jsonc
{ "Id": "<chan>", "Name": "La 1", "Type": "TvChannel", "MediaType": "Video",
  "ChannelType": "TV", "IsFolder": false, "Number": "1",
  "ImageTags": { "Primary": "primary" }, "ServerId": "…" }
```
MediaSource HLS (en PlaybackInfo y LiveStreams/Open):
```jsonc
{ "Id": "<chan>", "Protocol": "Http", "Path": "<url o /Videos/<id>/stream>",
  "Container": "hls", "IsInfiniteStream": true, "IsRemote": true,
  "SupportsDirectPlay": true, "SupportsDirectStream": true,
  "SupportsTranscoding": false, "RequiresOpening": false }
```
- **Vista**: en `jfGetViews`/`jfGetItem`, una librería `tv` → `CollectionType: "livetv"`.
- **Logos**: `jfGetItemImage` con rama canal → redirect a `tvg-logo` (o proxy con caché
  opcional en `covers/`; por defecto passthrough para respetar "0 escrituras a disco").

### D5b — Jerarquía de 3 niveles y visibilidad condicional
La librería TV es una **biblioteca en la raíz** como las demás, con jerarquía de 3 niveles:

```
TV (biblioteca, solo visible si habilitada y con fuente válida)
└── Generalistas        (nivel 2: categoría = group-title)
    └── La 1            (nivel 3: canal = tvg-name + logo + URL HLS)
└── Deportivos
    └── Real Madrid TV
```

**Regla de visibilidad (clave):** la biblioteca TV **no se muestra** salvo que
`content_type: tv` esté configurado **y** el `ChannelStore` haya parseado ≥1 canal para
ella (fuente válida). Implementación:
- `tv.ChannelCount(libIndex) int` en el store.
- En `jfGetViews`/`jfGetItem` (Jellyfin) y `BrowseLibraries` (WebUI), al iterar librerías:
  `if lib.ContentType == "tv" && tv.ChannelCount(i) == 0 { continue }`.
- Así, si el `.m3u` falta, está vacío o no parsea, la entrada TV simplemente no aparece
  (sin romper el resto de bibliotecas). Esto encaja con el modelo sin estado de RMS: la
  validez se re-evalúa en cada `Populate` (boot/rescan).

Niveles 2 y 3 reutilizan el browse por `parentId` ya existente
([jfGetItems](internal/server/jf_items.go#L45)): `parentId = libraryID(tv)` → grupos
(carpetas), `parentId = groupID` → canales. Sin tocar el flujo, solo añadiendo las ramas
de resolución del D3.

### D6 — API nativa + WebUI (navegador)
El WebUI usa `/api/v1/browse?path=` y `/api/v1/stream/` ([app.js](web/js/app.js)).
- **Browse**: `handleBrowse` reconoce paths sintéticos:
  `tv:<libIndex>` (raíz → grupos como carpetas) y `tv:<libIndex>/<group>` (→ canales),
  saltándose `IsPathAllowed`/`ReadDir`. Los canales se devuelven como `BrowseItem` con
  `thumbnail` = logo, `name` = `tvg-name`, y un campo nuevo `stream_type: "hls"`.
- **Reproducción**: `loadVideo` para un canal pone el `src` de video.js con
  `type: 'application/x-mpegURL'` (no `video/mp4`) para que VHS reproduzca HLS. El
  `video.min.js` empaquetado (687 KB) **ya incluye VHS/http-streaming**, así que no hace
  falta dependencia nueva. El stream apunta a `/api/v1/stream/tv:chan:<id>` que
  redirige/proxya.
- **Player en vivo sin controles** (decisión del usuario): se reutiliza el **mismo**
  `video.js` del WebUI, pero para canales se reproduce en modo "directo en vivo": ocultar
  la barra de progreso/seek y los controles de pausa (sin pausa ni grabación — eso queda
  fuera de alcance). Implementación: al abrir un canal, configurar el player con
  `controlBar` reducido (solo volumen + pantalla completa) o `controls:false` con un overlay
  mínimo, y reactivar los controles normales al volver a VOD. Al ser `IsInfiniteStream`,
  no hay duración ni seek de todos modos; el override de duración (`installDurationOverride`)
  se salta para canales.

---

## Fases (incremental, demoable pronto)

| Fase | Alcance | Resultado visible | Riesgo |
|---|---|---|---|
| **0 ✅** | Parser M3U + `ChannelStore` (con **fusión de fuentes** por identidad tvg-id/nombre) + config `content_type: tv`, integrado en `Populate`. Tests con `lists/tv.m3u`. **HECHO** (`internal/tv/`). | (interno) | Bajo |
| **1 ✅** | WebUI: browse de categorías/canales + reproducción HLS por **redirect**, logos vía proxy, player live sin barra de seek. **HECHO** (`internal/server/tv.go`, `web/js/app.js`). | **ver y reproducir canales en el navegador**. | Bajo |
| **2** | API LiveTv para clientes Jellyfin: `Info`, `Channels`, `PlaybackInfo`, `LiveStreams/Open`, logos. | Pestaña Live TV en Streamyfin/Kodi. | Medio |
| **3** | Modo **proxy** para streams con cabeceras `#EXTVLCOPT`/CORS, caché opcional de logos. **Quitar el soporte JSON** (`parse_json.go` + rama en `parseByName`): el 99% de las listas son `.m3u`/`.m3u8`; el JSON añade superficie sin uso real. | Robustez (canales que hoy fallan por cabeceras). | Medio-alto |
| **5** | Config de **fuentes múltiples por biblioteca** + `refresh`/`refresh_interval` por fuente (re-descarga de listas remotas). | Listas que se actualizan solas. | Medio |

## Coste estimado

- **Parser + store**: ~150-250 LOC, riesgo bajo.
- **Config + validación**: ~30 LOC.
- **Resolución de IDs** (ramas en handlers existentes): pequeño pero toca varios ficheros.
- **Handlers LiveTv** (Info, Channels, Channel, rama PlaybackInfo, LiveStreams/Open, imagen): ~250-350 LOC.
- **Streaming**: redirect trivial; **proxy HLS ~150-250 LOC** (lo más delicado).
- **Browse nativo + WebUI**: ~150 LOC JS + rama de browse.
- **Docs/ADR-015**.

**Total**: ~1000-1400 LOC en ~10 ficheros. Repartible en las 4 fases; tras la **Fase 1**
ya se ven y reproducen canales en el WebUI. Las Fases 0-2 cubren el caso común (HLS
estándar sin cabeceras); la Fase 3 es la que da robustez con streams "difíciles".

## Ficheros afectados

- `internal/tv/` (nuevo): `parse_m3u.go`, `parse_json.go`, `store.go` (+ tests).
- [internal/config/config.go](internal/config/config.go): `Library.TVProxy`, `TVRefreshHours`, validación.
- [internal/server/server.go](internal/server/server.go): rutas LiveTv reales + `/tv/proxy`; `Populate` en `Start()`.
- [internal/server/autoscan.go](internal/server/autoscan.go): `tv.Populate` en `rescanLibraries()`.
- `internal/server/jf_livetv.go` (nuevo): handlers LiveTv + ramas canal en PlaybackInfo/stream/imagen.
- [internal/server/jf_items.go](internal/server/jf_items.go), [jf_helpers.go](internal/server/jf_helpers.go): vista `livetv`, ramas de item canal.
- [internal/server/browse.go](internal/server/browse.go) / `internal/media/browse.go`: paths `tv:` sintéticos.
- [web/js/app.js](web/js/app.js): tipo HLS en `loadVideo`, render de canales.
- `docs/adr/015-live-tv-from-m3u.md` (nuevo, revisa ADR-005).

## Riesgos / preguntas abiertas
1. **CORS en navegador**: muchos streams no permiten origen cruzado → en el WebUI puede
   hacer falta el proxy (Fase 3) antes que el redirect para varios canales.
2. **Cabeceras `#EXTVLCOPT`**: el redirect no las transporta; esos canales requieren proxy.
3. **EPG**: se deja fuera (programas vacíos). Posible extensión futura leyendo `url-tvg`
   (XMLTV) de la cabecera del M3U.
4. **Concurrencia en proxy**: N espectadores = N conexiones abiertas; aceptable en uso
   doméstico, a vigilar en una Pi.
