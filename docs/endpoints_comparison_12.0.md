# Comparación de endpoints: Jellyfin 10.11.9 → 12.0-rc1 (solo endpoints implementados)

**Fecha:** 2026-06-26
**Specs comparados:** `specs/jellyfin-openapi-10.11.9.json` (v10.11.9) vs `specs/jellyfin-openapi-12.0-rc1.json` (v12.0.0)
**Objetivo:** detectar si **parámetros** o **forma de respuesta** cambian en los endpoints que RMS ya implementa, de cara a compatibilidad con 12.0.

## Metodología y alcance

- Se extrajo de [server.go](../internal/server/server.go) la lista de rutas Jellyfin registradas (303 pares método+ruta), se normalizaron los nombres de parámetros de ruta y se filtraron las rutas internas `/api/v1`.
- Se cruzó con el spec 12.0-rc1 → **246 operaciones implementadas que también existen en 12.0** (la intersección que realmente importa).
- Para cada una se comparó entre 10.11.9 y 12.0-rc1: `parameters` (nombre, ubicación, requerido, tipo), `requestBody` y los schemas de cada código de respuesta.
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

**Conclusión: 12.0-rc1 es prácticamente compatible a nivel de firma con lo ya implementado.** Todos los cambios relevantes son aditivos (nuevos query params opcionales, nuevos campos en DTOs), que un servidor tolerante ignora sin romper clientes.

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
4. **Sin cambios obligatorios:** ningún cambio de 12.0-rc1 rompe los endpoints implementados.
