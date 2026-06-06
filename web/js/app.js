document.addEventListener('DOMContentLoaded', () => {
    const state = { sessionToken: '', username: '', currentPath: '', libraries: [], streamStrategies: ['direct', 'remux', 'transcode'], serverStrategies: ['direct', 'remux', 'transcode'], currentStrategyIndex: 0, currentBg: null, subtitleOffset: 0, subs: [], currentVideoPath: null, nextEpisodePath: null, nextEpisodeTimer: null, prefs: null };
    const $ = id => document.getElementById(id);
    const loginScreen = $('login-screen'), appContainer = $('app-container'), loginForm = $('login-form');
    const fileBrowser = $('file-browser'), videoPlayerModal = $('video-player-modal');
    const bgImage = $('bg-image'), metadataContainer = $('metadata-container');
    const player = videojs('video-player', {
        fluid: true,
        // Disable the built-in subtitle styling dialog: user prefs (rms-prefs-*) own
        // cue style via ::cue, and TextTrackSettings would inject inline styles that
        // override them.
        textTrackSettings: false,
        persistTextTrackSettings: false,
    });

    function showLoading(text) { $('loading-text').textContent = text; $('loading-overlay').style.display = 'flex'; }
    function hideLoading() { $('loading-overlay').style.display = 'none'; }

    function toast(text, type = 'error') {
        const el = document.createElement('div');
        el.className = `toast toast-${type}`;
        el.textContent = text;
        document.body.appendChild(el);
        setTimeout(() => el.remove(), 4000);
    }

    function setBg(url) {
        if (state.currentBg) URL.revokeObjectURL(state.currentBg);
        if (url) {
            fetchWithAuth(url).then(r => r.blob()).then(b => {
                const u = URL.createObjectURL(b);
                bgImage.style.backgroundImage = `url(${u})`;
                bgImage.classList.add('visible');
                state.currentBg = u;
            });
        } else { bgImage.classList.remove('visible'); state.currentBg = null; }
    }

    // Search
    $('search-input').addEventListener('input', e => {
        const q = e.target.value.toLowerCase();
        document.querySelectorAll('.file-item').forEach(el => {
            el.style.display = el.querySelector('.item-name').textContent.toLowerCase().includes(q) ? '' : 'none';
        });
    });

    // Login
    loginForm.addEventListener('submit', async e => {
        e.preventDefault(); showLoading('Signing in...');
        try {
            const resp = await fetch('/api/v1/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'include',
                body: JSON.stringify({ username: $('username-input').value.trim() || 'rms', password: $('password-input').value })
            });
            if (!resp.ok) throw new Error('Incorrect username or password');
            const data = await resp.json();
            state.sessionToken = data.token;
            state.username = data.username;
            await enterApp();
            hideLoading();
        } catch (err) { hideLoading(); $('login-error').textContent = err.message; }
    });

    async function enterApp() {
        loginScreen.style.display = 'none';
        appContainer.style.display = 'block';
        await loadServerConfig();
        loadPrefs();
        applyPrefs();
        $('settings-username').textContent = state.username;
        browseFiles('');
    }

    async function fetchWithAuth(url, opts = {}) {
        const headers = { ...opts.headers };
        if (state.sessionToken) headers.Authorization = `Bearer ${state.sessionToken}`;
        const resp = await fetch(url, { ...opts, headers, credentials: 'include' });
        if (resp.status === 401) { toast('Session expired'); globalThis.location.reload(); }
        return resp;
    }

    // Try cookie-based auto-login
    async function tryAutoLogin() {
        try {
            const resp = await fetch('/api/v1/me', { credentials: 'include' });
            if (!resp.ok) return false;
            const data = await resp.json();
            state.username = data.username;
            await enterApp();
            return true;
        } catch { return false; }
    }

    // Browse
    async function browseFiles(path) {
        state.currentPath = path; showLoading('Loading...');
        try {
            const data = await (await fetchWithAuth(`/api/v1/browse?path=${encodeURIComponent(path)}`)).json();
            const items = data.items || [], folder = data.current_folder;
            if (!path || path === '.') state.libraries = items.map(i => ({ friendly_name: i.friendly_name, path: i.path }));
            setBg(folder?.backdrop || null);
            renderBreadcrumb(path);
            renderMetadata(folder);
            renderFiles(items);
            $('crawl-actions').style.display = path && path !== '.' ? 'flex' : 'none';
            items.filter(i => !i.is_dir).forEach(i => {
                loadSubs(i.path);
                if (!i.metadata?.runtime) loadDuration(i.path);
            });
            hideLoading();
        } catch (err) { hideLoading(); fileBrowser.innerHTML = `<p style="color:#ef5350">Error: ${err.message}</p>`; }
    }

    function getBackPath() {
        if (!state.currentPath || state.currentPath === '.') return null;
        // Find parent: if it's a library root go home, otherwise go up one level
        const lib = state.libraries.find(l => state.currentPath === l.path);
        if (lib) return '';
        const parent = state.currentPath.substring(0, state.currentPath.lastIndexOf('/'));
        return parent || '';
    }

    function renderMetadata(folder) {
        const backPath = getBackPath();
        const backBtn = backPath !== null ? `<a href="#" class="back-btn" id="back-btn">&#8592;</a>` : '';

        if (!folder?.metadata) {
            if (backPath !== null) {
                // Show folder name + back button even without NFO
                const dirName = state.currentPath.split('/').pop() || '';
                metadataContainer.innerHTML = `<div class="meta-hero"><h2>${backBtn} ${dirName}</h2></div>`;
                $('back-btn')?.addEventListener('click', e => { e.preventDefault(); browseFiles(backPath); });
            } else {
                metadataContainer.innerHTML = '';
            }
            return;
        }
        const m = folder.metadata;
        let html = '<div class="meta-hero">';
        if (folder.logo) html += `<img class="meta-logo" id="meta-logo" src="">`;
        html += `<h2>${backBtn} ${m.title || m.original_title || ''}</h2>`;
        if (m.year) html += `<div class="meta-year">${m.year}</div>`;
        if (m.plot) html += `<div class="meta-plot">${m.plot}</div>`;
        html += '<div class="meta-badges">';
        if (m.rating) html += `<span class="badge badge-rating">&#9733; ${m.rating.toFixed?.(1) ?? m.rating}</span>`;
        if (m.runtime) html += `<span class="badge badge-runtime">${m.runtime} min</span>`;
        if (m.studio) html += `<span class="badge badge-studio">${m.studio}</span>`;
        if (m.genres?.length) m.genres.forEach(g => html += `<span class="badge badge-genre">${g}</span>`);
        html += '</div></div>';
        metadataContainer.innerHTML = html;
        if (folder.logo) fetchWithAuth(folder.logo).then(r=>r.blob()).then(b=> { $('meta-logo').src = URL.createObjectURL(b); });
        $('back-btn')?.addEventListener('click', e => { e.preventDefault(); browseFiles(backPath); });
    }

    function renderFiles(items) {
        fileBrowser.innerHTML = items.map(item => {
            const id = `icon-${item.path.replace(/[^a-zA-Z0-9]/g, '-')}`;
            const name = item.friendly_name || item.name;
            const durId = !item.is_dir ? `dur-${item.path.replace(/[^a-zA-Z0-9]/g, '-')}` : '';
            let detail = '';
            if (item.metadata?.runtime) detail = `<div class="item-year">${item.metadata.runtime} min</div>`;
            else if (item.metadata?.year) detail = `<div class="item-year">${item.metadata.year}</div>`;
            else if (!item.is_dir) detail = `<div class="item-year" id="${durId}"></div>`;
            const subsId = !item.is_dir ? `subs-${item.path.replace(/[^a-zA-Z0-9]/g, '-')}` : '';
            let poster;
            if (item.name === '..') poster = `<div class="item-poster" id="${id}">&#8617;</div>`;
            else if (item.is_dir) poster = `<div class="item-poster" id="${id}">&#128193;</div>`;
            else poster = `<div class="item-poster" id="${id}">&#127916;</div>`;
            return `<div class="file-item${item.name === '..' ? ' back-item' : ''}" data-path="${item.path}" data-is-dir="${item.is_dir}">
                ${poster}
                <div class="item-info">
                    <div class="item-name" title="${name}">${name}</div>
                    ${detail}
                    ${!item.is_dir ? `<div class="item-subs" id="${subsId}"></div>` : ''}
                </div>
            </div>`;
        }).join('');

        items.forEach(item => {
            const src = item.is_dir ? item.icon : item.thumbnail;
            if (!src) return;
            const el = document.getElementById(`icon-${item.path.replace(/[^a-zA-Z0-9]/g, '-')}`);
            if (!el) return;
            fetchWithAuth(src).then(r => r.blob()).then(b => {
                el.innerHTML = `<img src="${URL.createObjectURL(b)}">`;
            }).catch(() => {});
        });
    }

    function renderBreadcrumb(path) {
        const bc = $('breadcrumb');
        if (!path || path === '.') { bc.innerHTML = '<a href="#" data-path="">Home</a>'; return; }
        let html = '<a href="#" data-path="">Home</a>';
        const lib = state.libraries.find(l => path.startsWith(l.path));
        if (lib) {
            html += ` / <a href="#" data-path="${lib.path}">${lib.friendly_name}</a>`;
            const sub = path.substring(lib.path.length).replace(/^\/|\/$/g, '');
            if (sub) { let cur = lib.path; sub.split('/').forEach(p => { cur += `/${p}`; html += ` / <a href="#" data-path="${cur}">${p}</a>`; }); }
        }
        bc.innerHTML = html;
    }

    // Video Player
    function playVideo(path) {
        state.currentStrategyIndex = 0;
        videoPlayerModal.style.display = 'flex';
        player.off('error', handleVideoError);
        player.off('timeupdate', trackPlaybackProgress);
        player.off('loadedmetadata', applyKnownDuration);
        state.skipAttempts = 0;
        state.lastGoodTime = 0;
        state.lastRecoveryAt = 0;
        if (state.recoveryTimer) { clearTimeout(state.recoveryTimer); state.recoveryTimer = null; }
        player.on('timeupdate', trackPlaybackProgress);
        player.on('error', handleVideoError);
        player.on('loadedmetadata', applyKnownDuration);
        installDurationOverride();
        loadVideo(path);
    }

    // Override player.duration() for streamed sources (remux/transcode) where the
    // browser only sees a buffered duration. We probe the real duration on the
    // server and substitute it whenever it's greater than what the tech reports.
    let durationOverrideInstalled = false;
    function installDurationOverride() {
        if (durationOverrideInstalled) return;
        const origDuration = player.duration.bind(player);
        const origCurrentTime = player.currentTime.bind(player);
        player.duration = function (val) {
            // Delegate setter calls so video.js can keep its internal duration cache fresh.
            if (arguments.length > 0) return origDuration(val);
            const real = origDuration();
            if (state.knownDuration && state.knownDuration > (real || 0)) return state.knownDuration;
            return real;
        };
        // After a "nuclear" reseek the pipe restarts at 0 from the player's
        // POV. We add playbackOffset so the UI (progress bar, time display,
        // subtitle cue timing) keeps showing absolute file time.
        player.currentTime = function (val) {
            if (arguments.length > 0) return origCurrentTime(val);
            const raw = origCurrentTime();
            return (state.playbackOffset || 0) + (raw || 0);
        };
        durationOverrideInstalled = true;
    }
    function applyKnownDuration() {
        if (!state.knownDuration) return;
        // Force video.js to refresh its progress bar / time display.
        player.trigger('durationchange');
    }

    function trackPlaybackProgress() {
        if (player.readyState() < 2) return;
        const t = player.currentTime();
        if (t > state.lastGoodTime) state.lastGoodTime = t;
        // Reset skip counter once we've had 5s of wallclock-time without new errors.
        if (state.skipAttempts > 0 && Date.now() - state.lastRecoveryAt > 5000) {
            console.log('[player] clean playback resumed, resetting skip counter');
            state.skipAttempts = 0;
        }
    }

    function handleVideoError() {
        const err = player.error();
        if (!err) return;

        const ct = player.currentTime() || 0;
        const anchor = Math.max(state.lastGoodTime, ct);
        console.warn('[player] handleVideoError', { code: err.code, ct, anchor, attempts: state.skipAttempts });

        // Initial-load failure (currentTime ~ 0 and never had good time): rotate stream strategy.
        if (anchor < 1 && err.code === 4 && state.currentVideoPath) {
            state.currentStrategyIndex++;
            if (state.currentStrategyIndex < state.streamStrategies.length) loadVideo(state.currentVideoPath);
            else { hideLoading(); toast('All strategies failed'); }
            return;
        }

        // Give up after enough failed skips — usually a totally broken file or huge corrupt region.
        if (state.skipAttempts >= 15) {
            if (state.skipAttempts === 15) { toast('Too many decode errors, giving up'); state.skipAttempts++; }
            return;
        }

        // Grace period for decode errors (code 3 = MEDIA_ERR_DECODE). The
        // browser sometimes conceals corruption on its own and resumes within
        // a few hundred ms; waiting avoids an unnecessary reload/skip that
        // would interrupt audio. Code 4 (NETWORK/SRC) never self-heals.
        if (err.code === 3 && !state.recoveryTimer) {
            state.recoveryTimer = setTimeout(() => {
                state.recoveryTimer = null;
                const tNow = player.currentTime() || 0;
                if (tNow > ct + 0.1 && !player.error()) {
                    console.log('[player] decode error self-healed, no skip needed');
                    return;
                }
                performSkipRecovery();
            }, 500);
            return;
        }

        performSkipRecovery();
    }

    function performSkipRecovery() {
        const ct = player.currentTime() || 0;
        const anchor = Math.max(state.lastGoodTime, ct);

        state.skipAttempts++;
        state.lastRecoveryAt = Date.now();
        const myAttempt = state.skipAttempts;
        // Skip distance grows with attempts to escape longer corrupt regions: 3, 4, 5, 6, ...
        const skipDistance = 2 + myAttempt;
        const seekTo = anchor + skipDistance;
        console.warn(`[player] recovery #${myAttempt}: skipping ${skipDistance}s -> ${seekTo.toFixed(1)}s`);

        // Nuclear recovery: after repeated failures, restart ffmpeg from the
        // target time via ?start=. Only useful for pipe modes (remux/transcode
        // without a cache hit) — the standard Range-based seek below can't
        // escape a corrupt region on a non-seekable pipe. Direct and cached
        // remux keep using Range seek (faster, no restart cost).
        const strategy = state.streamStrategies[state.currentStrategyIndex];
        if (strategy !== 'direct' && myAttempt >= 3) {
            nuclearReseek(seekTo);
            return;
        }

        const src = player.currentSrc();
        player.one('loadedmetadata', () => {
            // A newer attempt has been queued: bail and let it handle the seek.
            if (state.skipAttempts !== myAttempt) return;
            player.currentTime(seekTo);
            // video.js clears remote text tracks when src changes, even when
            // the new URL is identical. Re-attach them after the reload.
            for (const sub of state.subs) mountSubtitleTrack(sub);
            player.play().catch(() => {});
        });
        player.src({ src, type: 'video/mp4' });

        if (myAttempt === 1) toast('Skipping corrupted region...');
    }

    function nuclearReseek(t) {
        state.playbackOffset = t;
        const strategy = state.streamStrategies[state.currentStrategyIndex];
        const safePath = state.currentVideoPath.startsWith('/') ? state.currentVideoPath.substring(1) : state.currentVideoPath;
        const url = `/api/v1/stream/${encodeURIComponent(safePath)}?strategy=${strategy}&start=${t}&token=${encodeURIComponent(state.sessionToken)}`;
        console.warn(`[player] nuclear reseek -> ffmpeg restart at ${t.toFixed(1)}s`);
        toast('Restarting stream past corruption...');
        player.one('loadedmetadata', () => {
            // Re-mount subs AFTER the reload (video.js clears tracks on src change).
            // The shift uses playbackOffset so cues line up with the restarted stream.
            for (const sub of state.subs) mountSubtitleTrack(sub);
            player.play().catch(() => {});
        });
        player.src({ src: url, type: 'video/mp4' });
    }

    // Safari often fails silently (no 'error' event) for unsupported codecs/containers.
    // The watchdog detects "stuck loading" and rotates to the next strategy.
    function armLoadWatchdog(strategy) {
        clearLoadWatchdog();
        player.one('canplay', clearLoadWatchdog);
        player.one('playing', clearLoadWatchdog);
        state.loadWatchdog = setTimeout(() => {
            console.warn(`[player] watchdog: ${strategy} stuck loading, rotating strategy`);
            state.currentStrategyIndex++;
            if (state.currentStrategyIndex < state.streamStrategies.length) {
                toast(`${strategy} stuck, trying ${state.streamStrategies[state.currentStrategyIndex]}...`);
                loadVideo(state.currentVideoPath);
            } else {
                hideLoading();
                toast('All strategies failed');
            }
        }, 12000);
    }

    function clearLoadWatchdog() {
        if (state.loadWatchdog) {
            clearTimeout(state.loadWatchdog);
            state.loadWatchdog = null;
        }
    }

    async function loadVideo(path) {
        const strategy = state.streamStrategies[state.currentStrategyIndex];
        const safePath = path.startsWith('/') ? path.substring(1) : path;
        state.currentVideoPath = path;
        state.knownDuration = 0;
        state.playbackOffset = 0;
        hideNextEpisodePrompt();
        showLoading(`Loading (${strategy})...`);
        armLoadWatchdog(strategy);

        // For strategies that stream via pipe (remux/transcode), the player can't
        // read the total duration from the fMP4 moov. Pre-probe it and override.
        if (strategy !== 'direct') {
            fetchWithAuth(`/api/v1/duration/${encodeURIComponent(safePath)}`)
                .then(r => r.ok ? r.json() : null)
                .then(d => {
                    if (d?.seconds > 0) {
                        state.knownDuration = d.seconds;
                        player.trigger('durationchange');
                    }
                })
                .catch(() => {});
        }

        try {
            const videoUrl = `/api/v1/stream/${encodeURIComponent(safePath)}?strategy=${strategy}&token=${encodeURIComponent(state.sessionToken)}`;
            player.src({ src: videoUrl, type: 'video/mp4' });

            clearSubtitleTracks();
            state.subtitleOffset = 0;
            subOffsetSlider.value = 0;
            subOffsetValue.textContent = '+0.0 s';
            $('sub-offset-bar').classList.remove('visible');

            try {
                const subsResp = await fetchWithAuth(`/api/v1/subtitles-list/${encodeURIComponent(safePath)}`);
                if (subsResp.ok) {
                    const subs = await subsResp.json();
                    for (const sub of (subs || [])) {
                        const subResp = await fetchWithAuth(`/api/v1/subtitles/${encodeURIComponent(safePath)}?lang=${sub.language}`);
                        if (subResp.ok) {
                            const vttText = await subResp.text();
                            const entry = {
                                language: sub.language,
                                label: sub.label,
                                default: sub.language === 'en',
                                vttText,
                                blobUrl: null,
                                trackEl: null
                            };
                            mountSubtitleTrack(entry);
                            state.subs.push(entry);
                        }
                    }
                    if (state.subs.length) $('sub-offset-bar').classList.add('visible');
                }
            } catch {}
            hideLoading(); player.play();
        } catch { player.error({ code: 4, message: 'Failed' }); }
    }

    function clearSubtitleTracks() {
        for (const sub of state.subs) {
            try { player.removeRemoteTextTrack(sub.trackEl); } catch {}
            URL.revokeObjectURL(sub.blobUrl);
        }
        state.subs = [];
    }

    function closeModal() {
        videoPlayerModal.style.display = 'none'; player.pause(); player.off('error', handleVideoError); player.off('timeupdate', trackPlaybackProgress); player.off('loadedmetadata', applyKnownDuration); clearLoadWatchdog();
        if (state.recoveryTimer) { clearTimeout(state.recoveryTimer); state.recoveryTimer = null; }
        state.knownDuration = 0;
        const src = player.currentSrc();
        if (src?.startsWith('blob:')) URL.revokeObjectURL(src);
        player.src('');
        clearSubtitleTracks();
        state.currentVideoPath = null;
        hideNextEpisodePrompt();
        $('sub-offset-bar').classList.remove('visible');
    }

    // --- Subtitle offset (rebuilds VTT client-side so browsers refresh reliably) ---
    const subOffsetSlider = $('sub-offset-slider'), subOffsetValue = $('sub-offset-value');

    function parseVttTimestamp(ts) {
        const match = /^(\d{2}):(\d{2}):(\d{2})\.(\d{3})$/.exec(ts.trim());
        if (!match) return null;
        const [, hh, mm, ss, ms] = match;
        return (((Number(hh) * 60) + Number(mm)) * 60 + Number(ss)) + (Number(ms) / 1000);
    }

    function formatVttTimestamp(totalSeconds) {
        const clamped = Math.max(0, totalSeconds);
        let wholeMillis = Math.round(clamped * 1000);
        const hours = Math.floor(wholeMillis / 3600000);
        wholeMillis -= hours * 3600000;
        const minutes = Math.floor(wholeMillis / 60000);
        wholeMillis -= minutes * 60000;
        const seconds = Math.floor(wholeMillis / 1000);
        const millis = wholeMillis - (seconds * 1000);
        return [
            String(hours).padStart(2, '0'),
            String(minutes).padStart(2, '0'),
            `${String(seconds).padStart(2, '0')}.${String(millis).padStart(3, '0')}`
        ].join(':');
    }

    function shiftVttText(vttText, offset) {
        return vttText.replace(
            /^(\d{2}:\d{2}:\d{2}\.\d{3})\s+-->\s+(\d{2}:\d{2}:\d{2}\.\d{3})(.*)$/gm,
            (_, startTs, endTs, settings = '') => {
                const start = parseVttTimestamp(startTs);
                const end = parseVttTimestamp(endTs);
                if (start == null || end == null) return _;
                const shiftedStart = Math.max(0, start + offset);
                const shiftedEnd = Math.max(shiftedStart, end + offset);
                return `${formatVttTimestamp(shiftedStart)} --> ${formatVttTimestamp(shiftedEnd)}${settings}`;
            }
        );
    }

    function refreshVisibleSubtitle(track) {
        if (!track || track.mode !== 'showing') return;
        const originalMode = track.mode;
        track.mode = 'hidden';
        requestAnimationFrame(() => {
            track.mode = originalMode;
            if (player.paused()) {
                const now = player.currentTime();
                if (Number.isFinite(now)) player.currentTime(now);
            }
        });
    }

    function mountSubtitleTrack(entry) {
        const previousMode = entry.trackEl?.track?.mode || (entry.default ? 'showing' : 'disabled');
        if (entry.trackEl) {
            try { player.removeRemoteTextTrack(entry.trackEl); } catch {}
        }
        if (entry.blobUrl) URL.revokeObjectURL(entry.blobUrl);

        const shiftedText = shiftVttText(entry.vttText, state.subtitleOffset - (state.playbackOffset || 0));
        entry.blobUrl = URL.createObjectURL(new Blob([shiftedText], { type: 'text/vtt' }));
        entry.trackEl = player.addRemoteTextTrack({
            kind: 'subtitles',
            src: entry.blobUrl,
            srclang: entry.language,
            label: entry.label,
            default: entry.default
        }, false);

        const track = entry.trackEl.track;
        if (track) {
            track.mode = previousMode;
            entry.trackEl.addEventListener('load', () => refreshVisibleSubtitle(track), { once: true });
        }
    }

    function setSubtitleOffset(val) {
        state.subtitleOffset = Math.round(Math.max(-10, Math.min(10, val)) * 10) / 10;
        subOffsetSlider.value = state.subtitleOffset;
        subOffsetValue.textContent = `${state.subtitleOffset >= 0 ? '+' : ''}${state.subtitleOffset.toFixed(1)} s`;
        for (const sub of state.subs) mountSubtitleTrack(sub);
    }
    subOffsetSlider.addEventListener('input', e => setSubtitleOffset(Number.parseFloat(e.target.value)));
    $('sub-offset-minus').addEventListener('click', () => setSubtitleOffset(state.subtitleOffset - 0.1));
    $('sub-offset-plus').addEventListener('click', () => setSubtitleOffset(state.subtitleOffset + 0.1));
    $('sub-offset-reset').addEventListener('click', () => setSubtitleOffset(0));

    // --- Interactive subtitle search ---
    const subSearchModal = $('sub-search-modal');
    const subSearchResults = $('sub-search-results');
    const subSearchQuery = $('sub-search-query');
    const subSearchLang = $('sub-search-lang');

    function openSubSearch() {
        if (!state.currentVideoPath) return;
        subSearchQuery.value = '';
        subSearchResults.innerHTML = '<div class="empty">Press Search to look up subtitles for the current video.</div>';
        subSearchModal.style.display = 'flex';
        setTimeout(() => subSearchQuery.focus(), 50);
    }
    function closeSubSearch() { subSearchModal.style.display = 'none'; }

    function escapeHtml(s) {
        return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }

    function renderSubResults(results) {
        if (!results.length) {
            subSearchResults.innerHTML = '<div class="empty">No subtitles found. Try a different query or language.</div>';
            return;
        }
        // Sort: hash matches first, then by downloads desc.
        results.sort((a, b) => (b.moviehash_match - a.moviehash_match) || (b.downloads - a.downloads));

        subSearchResults.innerHTML = results.map((r, idx) => {
            const tags = [];
            if (r.moviehash_match) tags.push('<span class="tag hash">✓ Hash match</span>');
            if (r.from_trusted) tags.push('<span class="tag trusted">Trusted</span>');
            if (r.hd) tags.push('<span class="tag hd">HD</span>');
            if (r.fps) tags.push(`<span class="tag">${r.fps.toFixed(2)} fps</span>`);
            if (r.downloads) tags.push(`<span class="tag">${r.downloads.toLocaleString()} downloads</span>`);
            if (r.rating > 0) tags.push(`<span class="tag">★ ${r.rating.toFixed(1)}</span>`);
            if (r.hearing_impaired) tags.push('<span class="tag">SDH</span>');
            if (r.ai_translated) tags.push('<span class="tag warn">AI translated</span>');
            if (r.machine_translated) tags.push('<span class="tag warn">Machine translated</span>');
            if (r.uploader) tags.push(`<span class="tag">${escapeHtml(r.uploader)}</span>`);

            return `<div class="sub-result ${r.moviehash_match ? 'hash-match' : ''}" data-idx="${idx}">
                <div class="sub-result-main">
                    <div class="sub-result-name">${escapeHtml(r.file_name)}</div>
                    ${r.release ? `<div class="sub-result-release">${escapeHtml(r.release)}</div>` : ''}
                    <div class="sub-result-meta">${tags.join('')}</div>
                </div>
                <div class="sub-result-actions">
                    <button class="primary" data-action="download" data-idx="${idx}">Download</button>
                </div>
            </div>`;
        }).join('');

        // Stash results for the action handler.
        subSearchResults.dataset.results = JSON.stringify(results);
    }

    async function runSubSearch() {
        if (!state.currentVideoPath) return;
        subSearchResults.innerHTML = '<div class="loading">Searching…</div>';
        try {
            const resp = await fetchWithAuth(
                `/api/v1/subtitles-search/${encodeURIComponent(state.currentVideoPath)}`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        query: subSearchQuery.value.trim(),
                        languages: [subSearchLang.value]
                    })
                }
            );
            if (!resp.ok) {
                const err = await resp.json().catch(() => ({}));
                subSearchResults.innerHTML = `<div class="empty">Search failed: ${escapeHtml(err.error || resp.statusText)}</div>`;
                return;
            }
            renderSubResults(await resp.json());
        } catch (e) {
            subSearchResults.innerHTML = `<div class="empty">Search error: ${escapeHtml(e.message)}</div>`;
        }
    }

    async function downloadSelectedSubtitle(idx, btn) {
        let results;
        try { results = JSON.parse(subSearchResults.dataset.results || '[]'); } catch { results = []; }
        const r = results[idx];
        if (!r || !state.currentVideoPath) return;

        const orig = btn.textContent;
        btn.disabled = true;
        btn.textContent = 'Downloading…';
        try {
            const resp = await fetchWithAuth(
                `/api/v1/subtitles-download/${encodeURIComponent(state.currentVideoPath)}`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ file_id: r.file_id, language: r.language || subSearchLang.value })
                }
            );
            const data = await resp.json();
            if (!resp.ok) {
                toast(`Download failed: ${data.error || resp.statusText}`, 'error');
                btn.disabled = false; btn.textContent = orig;
                return;
            }
            toast(`Downloaded: ${data.filename}`, 'success');
            btn.textContent = '✓ Downloaded';
            await reloadSubtitleTracks(data.language);
            setTimeout(closeSubSearch, 500);
        } catch (e) {
            toast(`Download error: ${e.message}`, 'error');
            btn.disabled = false; btn.textContent = orig;
        }
    }

    // Replace the player's text tracks with whatever is currently on disk,
    // preserving the active subtitle offset, and switch to the freshly downloaded language.
    async function reloadSubtitleTracks(preferredLang) {
        if (!state.currentVideoPath) return;
        clearSubtitleTracks();
        try {
            const safePath = state.currentVideoPath;
            const subs = await (await fetchWithAuth(`/api/v1/subtitles-list/${encodeURIComponent(safePath)}`)).json();
            for (const sub of (subs || [])) {
                const subResp = await fetchWithAuth(`/api/v1/subtitles/${encodeURIComponent(safePath)}?lang=${sub.language}`);
                if (!subResp.ok) continue;
                const vttText = await subResp.text();
                const entry = {
                    language: sub.language,
                    label: sub.label,
                    default: sub.language === preferredLang,
                    vttText,
                    blobUrl: null,
                    trackEl: null
                };
                mountSubtitleTrack(entry);
                state.subs.push(entry);
            }
            if (state.subs.length) $('sub-offset-bar').classList.add('visible');
        } catch {}
    }

    $('sub-search-btn').addEventListener('click', openSubSearch);
    $('sub-search-close').addEventListener('click', closeSubSearch);
    $('sub-search-go').addEventListener('click', runSubSearch);
    subSearchQuery.addEventListener('keydown', e => { if (e.key === 'Enter') runSubSearch(); });
    subSearchResults.addEventListener('click', e => {
        const btn = e.target.closest('button[data-action="download"]');
        if (btn) downloadSelectedSubtitle(Number(btn.dataset.idx), btn);
    });
    subSearchModal.addEventListener('click', e => { if (e.target === subSearchModal) closeSubSearch(); });
    document.addEventListener('keydown', e => {
        if (e.key === 'Escape' && subSearchModal.style.display === 'flex') closeSubSearch();
    });

    // --- Next Episode ---
    const VIDEO_EXTS = ['.mkv', '.mp4', '.avi', '.m4v', '.mov', '.webm'];
    const NEXT_EP_COUNTDOWN = 10;
    const nextEpPrompt = $('next-ep-prompt'), nextEpTitle = $('next-ep-title'), nextEpCountdown = $('next-ep-countdown');

    async function findNextEpisode(currentPath) {
        const parent = currentPath.substring(0, currentPath.lastIndexOf('/'));
        if (!parent) return null;
        try {
            const resp = await fetchWithAuth(`/api/v1/browse?path=${encodeURIComponent(parent)}`);
            if (!resp.ok) return null;
            const data = await resp.json();
            const videos = (data.items || [])
                .filter(i => !i.is_dir && VIDEO_EXTS.some(ext => i.name.toLowerCase().endsWith(ext)))
                .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
            const idx = videos.findIndex(v => v.path === currentPath);
            if (idx === -1 || idx === videos.length - 1) return null;
            return videos[idx + 1];
        } catch { return null; }
    }
    function hideNextEpisodePrompt() {
        nextEpPrompt.classList.remove('visible');
        if (state.nextEpisodeTimer) { clearInterval(state.nextEpisodeTimer); state.nextEpisodeTimer = null; }
        state.nextEpisodePath = null;
    }
    function showNextEpisodePrompt(next) {
        state.nextEpisodePath = next.path;
        nextEpTitle.textContent = next.friendly_name || next.name;
        let remaining = NEXT_EP_COUNTDOWN;
        nextEpCountdown.textContent = remaining;
        nextEpPrompt.classList.add('visible');
        state.nextEpisodeTimer = setInterval(() => {
            remaining--;
            nextEpCountdown.textContent = remaining;
            if (remaining <= 0) {
                clearInterval(state.nextEpisodeTimer);
                state.nextEpisodeTimer = null;
                const path = state.nextEpisodePath;
                hideNextEpisodePrompt();
                if (path) playVideo(path);
            }
        }, 1000);
    }
    player.on('ended', async () => {
        if (!state.currentVideoPath) return;
        const next = await findNextEpisode(state.currentVideoPath);
        if (next) showNextEpisodePrompt(next);
    });
    $('next-ep-play').addEventListener('click', () => {
        const path = state.nextEpisodePath;
        hideNextEpisodePrompt();
        if (path) playVideo(path);
    });
    $('next-ep-cancel').addEventListener('click', hideNextEpisodePrompt);

    // Events
    fileBrowser.addEventListener('click', e => {
        const item = e.target.closest('.file-item');
        if (item) item.dataset.isDir === 'true' ? browseFiles(item.dataset.path) : playVideo(item.dataset.path);
    });
    $('breadcrumb').addEventListener('click', e => { e.preventDefault(); if (e.target.tagName === 'A') browseFiles(e.target.dataset.path); });
    videoPlayerModal.querySelector('.modal-close').addEventListener('click', closeModal);
    window.addEventListener('click', e => { if (e.target === videoPlayerModal) closeModal(); });
    document.addEventListener('keydown', e => { if (e.key === 'Escape' && videoPlayerModal.style.display === 'flex') closeModal(); });

    // Crawl functions
    async function runCrawl(endpoint, btnId, label) {
        const btn = $(btnId); btn.disabled = true;
        const origHTML = btn.innerHTML;
        btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:14px;height:14px;animation:spin 0.8s linear infinite"><circle cx="12" cy="12" r="10"/></svg> Working...`;
        try {
            const resp = await fetchWithAuth(`/api/v1/crawl/${endpoint}`, {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: state.currentPath })
            });
            const data = await resp.json();
            toast(data.success ? `${label} complete!` : `${label} failed`, data.success ? 'success' : 'error');
            if (data.success) browseFiles(state.currentPath);
        } catch (err) { toast(`${label} error: ${err.message}`); }
        finally { btn.innerHTML = origHTML; btn.disabled = false; }
    }
    window.setView = function(mode) {
        applyViewMode(mode);
        if (state.prefs) {
            state.prefs.view = mode;
            savePrefs();
        }
    };
    function applyViewMode(mode) {
        const grid = $('file-browser');
        $('view-grid').classList.toggle('active', mode === 'grid');
        $('view-list').classList.toggle('active', mode === 'list');
        grid.classList.toggle('list-view', mode === 'list');
    }

    window.crawlMetadata = () => runCrawl('metadata', 'btn-crawl-metadata', 'Metadata');
    window.crawlThumbnails = () => runCrawl('thumbnails', 'btn-crawl-thumbs', 'Thumbnails');

    window.crawlSubtitles = () => runCrawl('subtitles', 'btn-crawl-subtitles', 'Subtitles');

    async function loadDuration(videoPath) {
        try {
            const data = await (await fetchWithAuth(`/api/v1/duration/${encodeURIComponent(videoPath)}`)).json();
            if (!data?.minutes) return;
            const el = document.getElementById(`dur-${videoPath.replace(/[^a-zA-Z0-9]/g, '-')}`);
            if (el) el.textContent = `${data.minutes} min`;
        } catch {}
    }

    async function loadSubs(videoPath) {
        try {
            const subs = await (await fetchWithAuth(`/api/v1/subtitles-list/${encodeURIComponent(videoPath)}`)).json();
            if (!subs?.length) return;
            const el = document.getElementById(`subs-${videoPath.replace(/[^a-zA-Z0-9]/g, '-')}`);
            if (el) el.innerHTML = subs.map(s => `<span class="sub-tag">${s.label}</span>`).join('');
        } catch {}
    }

    // --- Server config & per-user preferences ---
    const DEFAULT_PREFS = {
        strategy: 'direct',
        subSize: 'm', subFont: 'sans', subColor: '#ffffff', subBg: 'semi', subEdge: 'outline',
        view: 'grid'
    };

    async function loadServerConfig() {
        try {
            const resp = await fetchWithAuth('/api/v1/config');
            if (!resp.ok) return;
            const data = await resp.json();
            if (Array.isArray(data.stream_strategy) && data.stream_strategy.length) {
                state.serverStrategies = data.stream_strategy.slice();
            }
        } catch {}
    }

    function prefsKey() { return `rms-prefs-${state.username || 'rms'}`; }

    function loadPrefs() {
        let stored = {};
        try { stored = JSON.parse(localStorage.getItem(prefsKey()) || '{}'); } catch {}
        state.prefs = { ...DEFAULT_PREFS, ...stored };
    }

    function savePrefs() {
        try { localStorage.setItem(prefsKey(), JSON.stringify(state.prefs)); } catch {}
    }

    function applyPrefs() {
        // Reorder stream strategies: preferred first, then the rest of the server-configured order.
        const preferred = state.prefs.strategy;
        const rest = state.serverStrategies.filter(s => s !== preferred);
        state.streamStrategies = state.serverStrategies.includes(preferred)
            ? [preferred, ...rest]
            : state.serverStrategies.slice();
        applySubtitleStyle();
        applySubtitlePreview();
        applyViewMode(state.prefs.view || 'grid');
        syncSettingsControls();
    }

    function subtitleStyleCSS() {
        const sizes = { s: '0.85em', m: '1em', l: '1.25em', xl: '1.5em' };
        const fonts = {
            sans: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
            serif: 'Georgia, "Times New Roman", serif',
            mono: '"SF Mono", Consolas, "Courier New", monospace'
        };
        const bg = state.prefs.subBg === 'none'
            ? 'transparent'
            : (state.prefs.subBg === 'solid' ? 'rgba(0,0,0,0.95)' : 'rgba(0,0,0,0.55)');
        const edge = state.prefs.subEdge === 'shadow'
            ? '2px 2px 4px rgba(0,0,0,0.9)'
            : (state.prefs.subEdge === 'none'
                ? 'none'
                : '1px 1px 2px #000, -1px -1px 2px #000, 1px -1px 2px #000, -1px 1px 2px #000');
        return {
            color: state.prefs.subColor || '#fff',
            background: bg,
            font: fonts[state.prefs.subFont] || fonts.sans,
            size: sizes[state.prefs.subSize] || sizes.m,
            edge
        };
    }

    function applySubtitleStyle() {
        const s = subtitleStyleCSS();
        let styleEl = document.getElementById('rms-cue-style');
        if (!styleEl) {
            styleEl = document.createElement('style');
            styleEl.id = 'rms-cue-style';
            document.head.appendChild(styleEl);
        }
        // video.js 8 renders cues via vtt.js polyfill: cues live in `.vjs-text-track-cue > div`
        // with inline styles from vtt.js (and previously TextTrackSettings). ::cue does not
        // apply since these aren't native browser cues, so we target the inline-styled div
        // directly with !important to win over vtt.js defaults.
        const cueRule = `color: ${s.color} !important; background-color: ${s.background} !important; font-family: ${s.font} !important; font-size: ${s.size} !important; text-shadow: ${s.edge} !important;`;
        styleEl.textContent = `
            .vjs-text-track-cue, .vjs-text-track-cue > * { ${cueRule} }
            ::cue { ${cueRule} }
        `;
    }

    function applySubtitlePreview() {
        const s = subtitleStyleCSS();
        const span = $('sub-preview')?.querySelector('span');
        if (!span) return;
        span.style.color = s.color;
        span.style.backgroundColor = s.background;
        span.style.fontFamily = s.font;
        span.style.fontSize = s.size;
        span.style.textShadow = s.edge;
    }

    function syncSettingsControls() {
        $('opt-strategy').value = state.prefs.strategy;
        $('opt-sub-size').value = state.prefs.subSize;
        $('opt-sub-font').value = state.prefs.subFont;
        $('opt-sub-color').value = state.prefs.subColor;
        $('opt-sub-bg').value = state.prefs.subBg;
        $('opt-sub-edge').value = state.prefs.subEdge;
    }

    // Settings modal wiring
    const settingsModal = $('settings-modal');
    $('btn-settings').addEventListener('click', () => { settingsModal.style.display = 'flex'; });
    $('settings-close').addEventListener('click', () => { settingsModal.style.display = 'none'; });
    settingsModal.addEventListener('click', e => { if (e.target === settingsModal) settingsModal.style.display = 'none'; });

    function bindPref(elId, prefKey, opts = {}) {
        const el = $(elId);
        if (!el) return;
        el.addEventListener(opts.event || 'change', () => {
            state.prefs[prefKey] = el.value;
            savePrefs();
            applyPrefs();
        });
    }
    bindPref('opt-strategy', 'strategy');
    bindPref('opt-sub-size', 'subSize');
    bindPref('opt-sub-font', 'subFont');
    bindPref('opt-sub-color', 'subColor', { event: 'input' });
    bindPref('opt-sub-bg', 'subBg');
    bindPref('opt-sub-edge', 'subEdge');

    $('btn-logout').addEventListener('click', async () => {
        try { await fetch('/api/v1/logout', { method: 'POST', credentials: 'include' }); } catch {}
        state.sessionToken = '';
        state.username = '';
        globalThis.location.reload();
    });

    // Boot: try cookie-based auto-login, otherwise show login screen
    tryAutoLogin();
});
