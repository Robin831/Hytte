# ui-preview — screenshot the app with real data

Runs a throwaway Hytte instance against a **copy** of the production database
with an injected session (user id 1), then drives headless Chromium to
screenshot or measure pages. Production is never touched; pages render with
real content (live Wordfeud games, actual lists) instead of empty states.

## One-time setup

```bash
cd tools/ui-preview
npm install                      # playwright (package.json here)
npx playwright install chromium  # ~115 MB into ~/.cache/ms-playwright
sudo npx playwright install-deps chromium   # system libs, once per box
```

## Use

```bash
# 1. Build the frontend you want to look at
cd web && npm run build && cd ..

# 2. Start the preview (builds the server from this checkout; foreground)
tools/ui-preview/preview.sh 8095 &

# 3. Screenshot pages at 1440px and 390px into /tmp/hytte-ui-preview/shots/
node tools/ui-preview/shoot.mjs '/wordfeud?tab=board' '/suggestions'

# 4. Hunt layout overflow (docWidth > viewport = horizontal scroll bug)
node tools/ui-preview/measure.mjs '/wordfeud?tab=board' 1440
```

## Cleanup — do not skip

`/tmp/hytte-ui-preview` holds a **database copy and a valid session token**
for it. Kill the server and delete the directory when done:

```bash
kill %1 && rm -rf /tmp/hytte-ui-preview
```

## Notes

- Port 8090 is taken by forge's web UI; default here is 8095.
- The wordfeud dictionary is symlinked from `~/Hytte/data/nsf2025.txt`; without
  it the solver shows "dictionary not available".
- Fixed-position elements (mobile hamburger) render at unreliable positions in
  fullPage captures — don't diagnose overlap bugs from them.
