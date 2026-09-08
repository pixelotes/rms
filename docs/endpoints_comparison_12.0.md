# Comparación de endpoints: Jellyfin 10.11.9 → 12.0 (solo endpoints implementados)

**Fecha:** 2026-06-26
**Specs comparados:** `specs/jellyfin-openapi-10.11.9.json` (v10.11.9) vs `specs/jellyfin-openapi-12.0.json` (v12.0.0)
**Objetivo:** detectar si **parámetros** o **forma de respuesta** cambian en los endpoints que RMS ya implementa, de cara a compatibilidad con 12.0.

## Metodología y alcance

- Se extrajo de [server.go](../internal/server/server.go) la lista de rutas Jellyfin registradas (303 pares método+ruta), se normalizaron los nombres de parámetros de ruta y se filtraron las rutas internas `/api/v1`.
- Se cruzó con el spec 12.0 → **246 operaciones implementadas que también existen en 12.0** (la intersección que realmente importa).
- Para cada una se comparó entre 10.11.9 y 12.0: `parameters` (nombre, ubicación, requerido, tipo), `requestBody` y los schemas de cada código de respuesta.
- Adicionalmente se diffearon los **schemas de componentes** referenciados directamente por las respuestas/bodies de esos endpoints (61 schemas), a nivel de nombre+tipo de propiedad de primer nivel.

**Limitaciones:** el diff de componentes cubre el primer nivel de propiedades de los 61 schemas referenciados directamente (no la clausura transitiva completa de DTOs anidados), y no detecta cambios de valores de `enum` ni de descripciones. Para un endpoint concreto crítico conviene revisar el schema a mano.

## Resumen

| Métrica | Valor |
|---|---|
| Operaciones implementadas ∩ existentes en 12.0 | 246 |
| Operaciones con **cambio de firma** (params/body/resp) | **16** |
| De ellas, cambios que **rompen** algo en RMS | **0** |
| Schemas de componentes referenciados con cambios | **3** (todos aditivos salvo 1 campo eliminado) |
| Rutas nuevas en 12.0 (no implementadas) | 1 (`GET /Items/{itemId}/Collections`) |

**Conclusión: 12.0 es prácticamente compatible a nivel de firma con lo ya implementado.** Todos los cambios relevantes son aditivos (nuevos query params opcionales, nuevos campos en DTOs), que un servidor tolerante ignora sin romper clientes.

## Cambios de firma por operación (16)

### Aditivos — nuevos parámetros opcionales (sin acción)
RMS puede ignorarlos; no afectan a respuestas existentes.

| Operación | Cambio |
|---|---|
| `GET /Items` | + query `audioLanguages` (string[]), `subtitleLanguages` (string[]) |
| `GET /Persons` | + query `nameStartsWith`, `nameStartsWithOrGreater`, `nameLessThan`, `parentId`, `startIndex` |
| `GET /System/ActivityLog/Entries` | + 10 query params de filtro/orden (`itemId`, `name`, `type`, `username`, `severity`, `sortBy`, `sortOrder`, `maxDate`, `overview`, `shortOverview`). RMS responde stub vacío → irrelevante. |

### Eliminación de parámetro (sin acción)
`breakOnNonKeyFrames` se elimina de todos los endpoints de streaming. RMS no lo usa (no hace segmentación HLS por keyframe), así que no afecta.

| Operación | Cambio |
|---|---|
| `GET/HEAD /Audio/{itemId}/stream` | − query `breakOnNonKeyFrames` |
| `GET/HEAD /Audio/{itemId}/stream.{container}` | − query `breakOnNonKeyFrames` |
| `GET/HEAD /Audio/{itemId}/universal` | − query `breakOnNonKeyFrames` |
| `GET/HEAD /Videos/{itemId}/stream` | − query `breakOnNonKeyFrames` |
| `GET/HEAD /Videos/{itemId}/stream.{container}` | − query `breakOnNonKeyFrames` |
| `GET /Shows/NextUp` | − query `disableFirstEpisode` |

### Cambios de tipo / respuestas (revisar pero bajo riesgo)

| Operación | Cambio | Impacto en RMS |
|---|---|---|
| `DELETE /Devices` | query `id`: pasa de `string` requerido → `string[]` opcional; respuesta de error `404` → `400` | RMS gestiona devices con stub; el handler debe aceptar `id` repetido/ausente. Bajo riesgo. |
| `POST /QuickConnect/Initiate` | respuesta `401` pasa de sin-schema → `ProblemDetails` | Solo formaliza el cuerpo de error; sin impacto. |

> Nota: las operaciones que **desaparecen** en 12.0 (`PlayingItems/*`, `Artists/InstantMix`, `Items/{}/CriticReviews`, `Videos/ActiveEncodings`, HLS legacy) se cubren en el análisis de rutas previo; mantenerlas implementadas es inofensivo (back-compat con 10.11).

## Cambios en schemas de componentes (3)

Todos los endpoints implementados que devuelven estos tipos heredan estos cambios:

| Schema | Cambio | Endpoints afectados (ej.) | Impacto |
|---|---|---|---|
| `BaseItemDto` | + `OriginalLanguage` (string), + `AlbumNormalizationGain` (number) | `GET /Items/{itemId}`, `GET /Items`, `GET /Users/{userId}/Items`, etc. | Aditivo. Opcional: poblar `OriginalLanguage` si se tiene en NFO. |
| `QueryFilters` | + `AudioLanguages` (array), + `SubtitleLanguages` (array) | `GET /Items/Filters` | Aditivo; se puede devolver vacío. |
| `SessionInfoDto` | − `NowPlayingQueueFullItems` (array) | `GET /Sessions` | RMS no lo emite → sin impacto. |

## Acciones recomendadas para anunciar 12.0

1. **Routing:** ✅ hecho. Se registró la única ruta nueva `GET /Items/{itemId}/Collections` → `jfEmptyItems` (devuelve `BaseItemDtoQueryResult` vacío; RMS no tiene colecciones por ADR-007). Ver [server.go](../internal/server/server.go).
2. **Versión anunciada:** hoy `jellyfin_version` por defecto es `10.11.0` ([config.go:168](../internal/config/config.go#L168)); para reportar 12.0 basta configurarlo. Los gates actuales son `jfVersionAtLeast(10, 11)` ([server.go:155](../internal/server/server.go#L155)); no hay gate `12.0` necesario.
3. **DTOs (opcional):** considerar añadir `OriginalLanguage` a `BaseItemDto` si se desea paridad de campos.
4. **Sin cambios obligatorios:** ningún cambio de 12.0 rompe los endpoints implementados.

## Auditoría código ↔ spec 12.0 (2026-09-08)

El análisis anterior comparaba spec contra spec. Esta pasada comparó **lo que RMS emite y lee realmente** con el spec 12.0, handler por handler, y se verificó con un validador que arranca RMS con `jellyfin_version: "12.0.0"` y contrasta cada respuesta JSON (claves, tipos, enums, `required`, nullabilidad y código HTTP) contra `specs/jellyfin-openapi-12.0.json`. Resultado final: **0 desviaciones** en modo 12.0, 0 en modo 10.11 (contra `10.11.9`) y 0 en modo 10.10 (contra `10.10.7`), salvo `GET /System/Configuration/encoding`, que el propio spec documenta como `string/binary` (peculiaridad conocida del generador).

Se corrigieron estos fallos, todos presentes también con el spec 10.11.9 (no eran específicos de 12.0):

| Endpoint | Problema | Corrección |
|---|---|---|
| `GET /System/Info/Storage` | Devolvía `[]`; el spec pide `SystemStorageDto` (objeto, 8 campos requeridos) | Objeto completo con carpetas y `Libraries` |
| `GET /Startup/Configuration` | `ServerName` no existe en `StartupConfigurationDto` de 10.10 | Se emite solo con `jellyfin_version >= 10.11` |
| `GET /System/Info/Public` | Clave `OperatingSystemDisplayName` fuera del schema | Eliminada |
| `GET /System/Configuration`, `GET /Startup/Configuration` | Claves fuera de `ServerConfiguration` / `StartupConfigurationDto` | Eliminadas |
| `GET /System/Configuration/encoding` | `EnableDeinterlace` no existe; `HardwareAccelerationType: ""` no es valor del enum | Eliminada / `"none"` |
| `GET /UserViews/GroupingOptions` | `{Name,Type}` en lugar de `SpecialViewOptionDto {Name,Id}` | `Id` |
| `GET /Localization/Options` | Objeto `{Countries,Cultures}`; el spec pide `LocalizationOption[]` | Array `{Name,Value}` |
| `GET /Movies/Recommendations` | Objeto suelto; el spec pide `RecommendationDto[]` | Array, con `BaselineItemName` y `CategoryId` |
| `GET /Library/VirtualFolders` | `CollectionType: "tvseries"` no es valor de `CollectionTypeOptions` | Mapeo a `tvshows` / `movies` / `null` |
| `GET /UserViews`, `GET /Items/{libId}` | Bibliotecas `content_type: tv` salían como `CollectionType: "movies"` | `"livetv"` |
| `GET /Items` sin `parentId` | Devolvía vacío; Jellyfin usa la raíz del usuario | Recursivo: todos los ítems; no recursivo: las vistas |
| `GET /Items/Filters2` | `Genres` como `string[]`; el spec pide `NameGuidPair[]` | Pares `{Name,Id}` (+ `AudioLanguages`/`SubtitleLanguages` solo en ≥12.0) |
| `GET /Items/Filters` | Faltaba `OfficialRatings` | Añadido |
| `GET /Search/Hints` | Faltaba `ItemId`; `ImageTags` no existe en `SearchHint` | `ItemId`, `MatchedTerm`, `PrimaryImageTag`, `Series`, `IndexNumber`… |
| `GET /Items/{id}/Ancestors` | 200 `[]` para ítem inexistente | 404 `ProblemDetails` |
| `GET /Items/{id}/MetadataEditor` | Clave `Item` fuera de `MetadataEditorInfo` | Eliminada; 404 si el ítem no existe |
| `GET /Items/{id}/ExternalIdInfos` | `UrlFormatString` fuera de `ExternalIdInfo` | Eliminada |
| `GET /MediaSegments/{id}` | Faltaban `TotalRecordCount`/`StartIndex` | Añadidos |
| `GET/POST /Items/{id}/PlaybackInfo` | `ErrorCode: "NoError"` no es valor de `PlaybackErrorCode`; `TranscodingUrl`/`TranscodingSubProtocol` no son propiedades de `PlaybackInfoResponse` | Eliminados |
| `MediaSourceInfo` (PlaybackInfo, ítems, Live TV) | `TranscodingSubProtocol: null` en enum no nullable; `SupportsThumbnails`, `DirectStreamUrl`, `PlaySessionId` no son propiedades; faltaba `UseMostCompatibleTranscodingProfile` | `"http"`/`"hls"`; claves eliminadas; campo añadido. Los `MediaStream` de Live TV ahora llevan el mismo conjunto de campos que los de vídeo |
| `MediaStream` | `IsOriginal` es nuevo en 12.0 | Se emite solo con `jellyfin_version >= 12.0` |
| `POST /UserItems/{id}/UserData` | Actualización parcial machacaba `PlaybackPositionTicks` a 0 (todos los campos de `UpdateUserItemDataDto` son nullable) | Solo se aplican los campos presentes; se soporta `Likes` |
| `POST/DELETE /UserItems/{id}/Rating` | 204 sin cuerpo; el spec pide 200 `UserItemDataDto` | Handler propio; `likes` se persiste |
| `POST /Users`, `POST /Auth/Keys`, `POST/DELETE /Branding/Splashscreen` | 200 con cuerpo o 404; el spec pide 204 | 204 |
| `POST /QuickConnect/Initiate`, `GET /QuickConnect/Connect`, `POST /QuickConnect/Authorize`, `POST /Users/AuthenticateWithQuickConnect` | 200 con un cuerpo que no coincide con `QuickConnectResult` / `boolean` / `AuthenticationResult` | 401 / 404 / 403 / 400, como Jellyfin con Quick Connect deshabilitado |
| `/LiveTv/*`, `/LiveStreams/*` | 404 sin biblioteca `tv`; el spec documenta 200 | Rutas siempre registradas (`IsEnabled: false`, listas vacías); `Timers`, `SeriesTimers`, `Recordings` añadidos como stubs vacíos |
| Errores en rutas Jellyfin | Cuerpo `{"error": …}` | `ProblemDetails` (RFC 7807), como declara el spec |
| `GET /GetUtcTime`, `GET/HEAD/POST/DELETE /UserImage`, `GET /FallbackFont/Fonts[/{name}]`, `GET /Years[/{year}]` | No registrados; jellyfin-web los llama tras el login (time-sync), al pintar el avatar, en el renderizador ASS y en los diálogos de filtro | Añadidos: `UtcTimeResponse`, 404/204, `[]`, `BaseItemDtoQueryResult` vacío |

Desviaciones conocidas que se mantienen a propósito (comportamiento, no forma):

- `userId` (query) se ignora en todos los endpoints: el usuario se deriva siempre del token. Correcto para clientes normales; un admin no puede consultar datos de otro usuario.
- Muchos parámetros de filtrado/orden (`sortOrder`, `excludeItemTypes`, `genres`, `years`, `isFavorite`, `season`, `adjacentTo`, parámetros de transcodificación…) se aceptan pero no se aplican. Los de redimensionado de imágenes (`maxWidth`, `maxHeight`, `quality`, `format`) se aplican solo si `app.cache_path` está configurado.
- `GET /System/Ping` devuelve `text/plain` sin comillas, igual que Jellyfin real.
