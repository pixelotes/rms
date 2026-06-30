# Plan: detección de contenido nuevo en RMS

> Objetivo: que el contenido recién añadido al disco se refleje en la API (incluido el
> Kodi Sync Queue incremental) sin penalizar una Raspberry Pi 3B (1 GB RAM).
> Filosofía del proyecto: **minimum overhead**.

## Contexto / diagnóstico

El núcleo es un **ID store** (`sync.Map`) que mapea `UUID (SHA-256 del path) → path`:

- `media.ItemID(path)` → hash determinista (no necesita el store).
- `media.ItemPath(id)` → **lookup inverso**; falla si el mapeo no está en el store.

El store solo se (re)puebla con `media.PopulateIDStore()` en 3 momentos:

1. Arranque — `Server.Start()` en [server.go:417](internal/server/server.go#L417).
2. Manual — `POST /api/v1/library/rescan` → `handleRescan` → `rescanLibraries()` en
   [autoscan.go:97-109](internal/server/autoscan.go#L97).
3. Auto-scan periódico — desactivado por defecto; granularidad de horas
   (`interval_hours`) o diaria (`schedule: "HH:MM"`), en [autoscan.go:13-95](internal/server/autoscan.go#L13).

**Por qué falla la detección:**
- Navegar usa `os.ReadDir` en vivo, así que un item nuevo puede aparecer al *browsear* su
  carpeta padre. Pero todo lo que resuelve **por ID** (reproducir, detalles, y el
  **Kodi Sync Queue incremental**) depende de `ItemPath(id)` → store obsoleto → no aparece.
- Las altas para Kodi solo se registran en `syncQueue.RecordAdded()` dentro de
  `rescanLibraries()` ([autoscan.go:101](internal/server/autoscan.go#L101)). Hasta que no corre un
  rescan, el contenido nuevo es invisible a la sync incremental.
- El auto-scan periódico **acopla** el refresh barato del store con los crawlers caros
  (metadata/subtitles/thumbnails), lo que empuja a intervalos largos en una Pi.

**Decisión de diseño:** se descarta `fsnotify`/inotify como solución por defecto porque
(a) no funciona en filesystems de red (SMB/NFS), habituales con NAS + Pi, y (b) no es
recursivo: un watch por directorio choca con `max_user_watches` y consume memoria de kernel.
Posible opt-in futuro solo para librerías en disco local.

---

## ⚠️ Paso 0 — Desbloquear la build (PRERREQUISITO)

El paquete `internal/media/` **no existe en este checkout y nunca se commiteó**. La regla
`media/` de [.gitignore](.gitignore) hace match con `internal/media/` y lo excluye. La build
falla: `package raspberry-media-server/internal/media is not in std`.

**Tareas:**
1. Recuperar el código de `internal/media/` (de otra máquina, backup, o imagen Docker
   `pixelotes/rms`: `docker create` + `docker cp` del binario no sirve para el fuente; buscar
   en una copia del repo donde sí exista).
2. Arreglar `.gitignore`: cambiar la línea `media/` por `/media/` (anclada a la raíz) para que
   solo ignore los media de test de la raíz y **no** `internal/media/`.
3. Verificar: `go build ./...` debe compilar sin errores.
4. Confirmar la firma real de `PopulateIDStore`. Las llamadas actuales asumen:
   ```go
   func PopulateIDStore(libs []config.Library) (added []string)
   ```
   Anotar si ya detecta bajas o solo altas (relevante para el Paso 4).

> **Sin el Paso 0 no se puede implementar ni probar nada de lo demás.**

---

## Paso 1 — Separar "refresh barato" de "crawl caro"

**Problema:** `runAutoScan()` mezcla el walk del ID store (barato) con lanzar crawlers
(caro). Queremos poder refrescar el índice a menudo sin penalización.

**Cambios en [internal/server/autoscan.go](internal/server/autoscan.go):**

El método `rescanLibraries()` (líneas 97-104) ya hace justo el refresh barato:
`PopulateIDStore` + `RecordAdded`. **Dejarlo como la única vía de "refresh de índice"** y
asegurarse de que NO lanza crawlers. `runAutoScan()` ya lo llama al final
([autoscan.go:93](internal/server/autoscan.go#L93)) — mantener esa composición:

```
runAutoScan()  = crawlers (caro, opcional) + rescanLibraries()
rescanLibraries() = SOLO PopulateIDStore + sync queue   ← reutilizable y barato
```

**Acción concreta:** no hace falta crear método nuevo; basta con garantizar la separación
y que `rescanLibraries()` sea el punto único que invocan Pasos 2 y 3. Si se quiere claridad,
renombrar a `refreshIndex()` y dejar `rescanLibraries()` como alias.

**Coste en Pi 3B:** un walk de unos miles de entradas con page cache caliente es
sub-segundo y despreciable en RAM (solo asignaciones transitorias de strings de paths).

**Criterio de aceptación:** llamar a `rescanLibraries()` no ejecuta `metacrawler` ni
`subcrawler` (no hay procesos hijos en `ps`), y el log dice "ID store refreshed ... N new items".

---

## Paso 2 — Webhook (camino instantáneo, overhead cero)

**Idea:** el endpoint ya existe — `POST /api/v1/library/rescan` →
[handleRescan](internal/server/autoscan.go#L106), registrado en
[server.go:86](internal/server/server.go#L86). Solo hay que hacerlo usable como webhook
externo (Sonarr/Radarr/qBittorrent post-script, cron, `inotifywait` del usuario, etc.).

**Problema de auth:** la ruta está bajo `protected` (`jwtMiddleware`: Bearer JWT o cookie de
sesión, [server.go:67](internal/server/server.go#L67)). Un *arr no tiene token de sesión.

**Cambios:**
1. **Token de webhook opcional.** Añadir a `AppConfig` en
   [config.go:58](internal/config/config.go#L58):
   ```go
   WebhookToken string `yaml:"webhook_token"` // si vacío, webhook deshabilitado
   ```
2. **Nueva ruta pública con auth por token** (no JWT). En `setupRoutes` de server.go, junto
   a las rutas públicas:
   ```go
   if s.config.App.WebhookToken != "" {
       api.HandleFunc("/library/rescan-hook", s.handleRescanHook).Methods("POST")
   }
   ```
3. **Handler `handleRescanHook`** (nuevo, en autoscan.go): comparar el token recibido
   (header `X-Webhook-Token` o query `?token=`) con `s.config.App.WebhookToken` usando
   `subtle.ConstantTimeCompare`; si coincide, llamar a `s.rescanLibraries()` y responder 200;
   si no, 401. Mantener la ruta `protected` `/library/rescan` para la UI.
4. **Debounce (recomendado):** si llegan varios webhooks seguidos (varios episodios), evitar
   N walks. Implementar un debounce simple: un `time.Timer` de ~5 s que se reinicia con cada
   hook y solo dispara `rescanLibraries()` al expirar. Guardar el timer en el `Server` con un
   `sync.Mutex`. Esto protege la Pi de ráfagas.

**Coste idle:** cero (no hay polling). Solo escanea cuando algo cambió de verdad.

**Documentación:** añadir a `docs/` un ejemplo de Custom Script en Sonarr/Radarr:
```sh
curl -fsS -X POST "http://rms:8096/api/v1/library/rescan-hook" -H "X-Webhook-Token: $TOKEN"
```

**Criterio de aceptación:** un POST con token válido refresca el store (item nuevo
reproducible por ID); token inválido → 401; ruta deshabilitada si `webhook_token` vacío;
ráfaga de 5 hooks en 2 s produce un solo walk.

---

## Paso 3 — Timer rescan-only opcional (fallback por polling)

**Idea:** para quien no pueda cablear un webhook (copias manuales, etc.), un ticker ligero
que ejecuta **solo** `rescanLibraries()` (sin crawlers), con granularidad de minutos,
independiente del `interval_hours` de los crawlers.

**Cambios en `AutoScanConfig` ([config.go:30](internal/config/config.go#L30)):**
```go
type AutoScanConfig struct {
    Enabled              bool   `yaml:"enabled"`
    Schedule             string `yaml:"schedule"`
    IntervalHours        int    `yaml:"interval_hours"`
    RescanIntervalMinutes int   `yaml:"rescan_interval_minutes"` // NUEVO: 0 = desactivado
    Metadata             bool   `yaml:"metadata"`
    Subtitles            bool   `yaml:"subtitles"`
    Thumbnails           bool   `yaml:"thumbnails"`
}
```

**Cambios en [autoscan.go](internal/server/autoscan.go):** en `startAutoScan()` (o en
`Start()`), arrancar un goroutine independiente si `RescanIntervalMinutes > 0`, **sin
depender de `Enabled`** (porque alguien puede querer solo el refresh barato sin crawlers):
```go
func (s *Server) startIndexRefresh() {
    m := s.config.Crawlers.AutoScan.RescanIntervalMinutes
    if m <= 0 { return }
    interval := time.Duration(m) * time.Minute
    log.Printf("Index refresh enabled: every %dm (rescan-only, no crawlers)", m)
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
            s.rescanLibraries()
        }
    }()
}
```
Llamar a `s.startIndexRefresh()` desde `Start()` ([server.go:415](internal/server/server.go#L415)),
junto a `startAutoScan()`. **Ojo:** que `startAutoScan` (Enabled) y `startIndexRefresh`
(RescanIntervalMinutes) no se solapen disparando rescans redundantes; documentar que son
independientes y que lo normal es usar uno u otro.

**Valor recomendado por defecto en docs/config de ejemplo:** `rescan_interval_minutes: 10`
(equilibrio frescura/coste en una Pi 3B). 0 = desactivado.

**Criterio de aceptación:** con `rescan_interval_minutes: 1` y `enabled: false`, añadir una
carpeta de película y ver que ≤1 min después es reproducible por ID y aparece en la sync
queue; los crawlers no se ejecutan.

---

## Paso 4 — Cubrir bajas además de altas

**Problema:** `rescanLibraries()` solo llama a `RecordAdded` ([autoscan.go:102](internal/server/autoscan.go#L102)).
Si se borra contenido, el Kodi Sync Queue nunca registra la baja (`RecordRemoved` existe en
[jf_kodi_sync.go:44](internal/server/jf_kodi_sync.go#L44) pero no se usa desde el rescan).

**Cambios:**
1. **`PopulateIDStore` debe devolver también las bajas.** Ajustar firma (en `internal/media/`,
   recuperado en Paso 0):
   ```go
   func PopulateIDStore(libs []config.Library) (added, removed []string)
   ```
   Implementación: comparar el set de paths del walk nuevo contra las keys actuales del
   `sync.Map`; `added` = paths nuevos, `removed` = IDs presentes en el store pero ya no en
   disco. Eliminar del store los `removed`.
2. **Actualizar `rescanLibraries()`:**
   ```go
   func (s *Server) rescanLibraries() {
       added, removed := media.PopulateIDStore(s.config.Libraries)
       log.Printf("Library rescan: +%d / -%d items", len(added), len(removed))
       if s.config.App.KodiSyncQueue {
           s.syncQueue.RecordAdded(added)
           s.syncQueue.RecordRemoved(removed)
       }
   }
   ```
3. Actualizar la otra llamada en [server.go:417](internal/server/server.go#L417) para la nueva
   firma (en boot no se registran deltas, solo se ignora `removed`).

**Consideración de memoria (Pi):** el slice `added`/`removed` y los listados de la sync queue
crecen sin límite. Verificar si `SyncQueueStore` purga entradas viejas; si no, añadir un TTL/
poda (p.ej. descartar cambios > 30 días en `Since`/al insertar) para no acumular RAM en
uptime largo. Anotar como sub-tarea si el store no lo hace ya.

**Criterio de aceptación:** borrar una carpeta y tras un rescan, su ID desaparece del store
(`ItemPath` da error) y se registra en `removed` de la sync queue.

---

## Orden de implementación sugerido

1. **Paso 0** (obligatorio, desbloquea todo).
2. **Paso 4** (corrige firma + bajas; afecta a la base reutilizada por 2 y 3).
3. **Paso 1** (garantizar separación barato/caro).
4. **Paso 2** (webhook — mayor valor, overhead cero).
5. **Paso 3** (timer fallback).

## Resumen de cambios de config (ejemplo final)

```yaml
app:
  webhook_token: "${RMS_WEBHOOK_TOKEN}"   # Paso 2; vacío = deshabilitado
crawlers:
  auto_scan:
    enabled: false                 # crawlers caros (metadata/subs/thumbs)
    interval_hours: 24
    rescan_interval_minutes: 10    # Paso 3; refresh barato del índice; 0 = off
    metadata: true
    subtitles: true
    thumbnails: false
```
