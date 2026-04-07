document.addEventListener('DOMContentLoaded', () => {
    const state = { sessionToken: '', currentPath: '', libraries: [], streamStrategies: ['direct', 'remux', 'transcode'], currentStrategyIndex: 0, currentBg: null };
    const $ = id => document.getElementById(id);
    const loginScreen = $('login-screen'), appContainer = $('app-container'), loginForm = $('login-form');
    const fileBrowser = $('file-browser'), videoPlayerModal = $('video-player-modal');
    const bgImage = $('bg-image'), metadataContainer = $('metadata-container');
    const player = videojs('video-player');

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
            const resp = await fetch('/api/v1/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: $('password-input').value }) });
            if (!resp.ok) throw new Error('Incorrect password');
            state.sessionToken = (await resp.json()).token;
            loginScreen.style.display = 'none'; appContainer.style.display = 'block';
            hideLoading(); browseFiles('');
        } catch (err) { hideLoading(); $('login-error').textContent = err.message; }
    });

    async function fetchWithAuth(url, opts = {}) {
        const headers = { ...opts.headers, Authorization: `Bearer ${state.sessionToken}` };
        const resp = await fetch(url, { ...opts, headers });
        if (resp.status === 401) { toast('Session expired'); window.location.reload(); }
        return resp;
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
        loadVideo(path);
        player.on('error', () => {
            if (player.error()?.code === 4) {
                state.currentStrategyIndex++;
                if (state.currentStrategyIndex < state.streamStrategies.length) loadVideo(path);
                else { hideLoading(); toast('All strategies failed'); }
            }
        });
    }

    async function loadVideo(path) {
        const strategy = state.streamStrategies[state.currentStrategyIndex];
        const safePath = path.startsWith('/') ? path.substring(1) : path;
        showLoading(`Loading (${strategy})...`);
        try {
            // Stream directly via URL with auth token as query param (no blob download)
            const videoUrl = `/api/v1/stream/${encodeURIComponent(safePath)}?strategy=${strategy}&token=${encodeURIComponent(state.sessionToken)}`;
            player.src({ src: videoUrl, type: 'video/mp4' });

            // Clear old subtitles
            const tracks = player.remoteTextTracks();
            for (let i = tracks.length - 1; i >= 0; i--) player.removeRemoteTextTrack(tracks[i]);

            // Load all available subtitles
            try {
                const subsResp = await fetchWithAuth(`/api/v1/subtitles-list/${encodeURIComponent(safePath)}`);
                if (subsResp.ok) {
                    const subs = await subsResp.json();
                    for (const sub of (subs || [])) {
                        const subResp = await fetchWithAuth(`/api/v1/subtitles/${encodeURIComponent(safePath)}?lang=${sub.language}`);
                        if (subResp.ok) {
                            const subBlob = await subResp.blob();
                            player.addRemoteTextTrack({
                                kind: 'subtitles',
                                src: URL.createObjectURL(subBlob),
                                srclang: sub.language,
                                label: sub.label,
                                default: sub.language === 'en'
                            }, false);
                        }
                    }
                }
            } catch {}
            hideLoading(); player.play();
        } catch { player.error({ code: 4, message: 'Failed' }); }
    }

    function closeModal() {
        videoPlayerModal.style.display = 'none'; player.pause(); player.off('error');
        const src = player.currentSrc();
        if (src?.startsWith('blob:')) URL.revokeObjectURL(src);
        player.src('');
        const tracks = player.remoteTextTracks();
        for (let i = tracks.length - 1; i >= 0; i--) {
            if (tracks[i].src?.startsWith('blob:')) URL.revokeObjectURL(tracks[i].src);
            player.removeRemoteTextTrack(tracks[i]);
        }
    }

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
        btn.innerHTML = origHTML; btn.disabled = false;
    }
    window.setView = function(mode) {
        const grid = $('file-browser');
        $('view-grid').classList.toggle('active', mode === 'grid');
        $('view-list').classList.toggle('active', mode === 'list');
        grid.classList.toggle('list-view', mode === 'list');
        localStorage.setItem('rms-view', mode);
    };
    // Restore saved view preference
    if (localStorage.getItem('rms-view') === 'list') {
        $('file-browser').classList.add('list-view');
        $('view-list')?.classList.add('active');
        $('view-grid')?.classList.remove('active');
    }

    window.crawlMetadata = () => runCrawl('metadata', 'btn-crawl-metadata', 'Metadata');
    window.crawlSubtitles = () => runCrawl('subtitles', 'btn-crawl-subtitles', 'Subtitles');
    window.crawlThumbnails = () => runCrawl('thumbnails', 'btn-crawl-thumbs', 'Thumbnails');

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
});
