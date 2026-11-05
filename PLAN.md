# Enhancement Plan

## 1️⃣ Lazy‑load poster images
- Add `loading="lazy"` attribute to the `<img>` tags that display file posters.
- This relies on native browser support and adds no runtime overhead.

## 2️⃣ Responsive grid breakpoints
- Update the `grid-template-columns` rule in `web/css/styles.css` to use `clamp()` for better width handling on very narrow screens.
- No additional media queries or JavaScript.

## 3️⃣ Lightweight Service‑Worker caching
- Create `web/sw.js` that precaches the main page and core assets.
- Register it in `web/js/app.js` after boot‑up.
- Keeps the UI responsive and works offline for the static assets.

All changes are committed in separate commits: `feat: lazy poster loading`, `feat: responsive grid`, `feat: service worker`.