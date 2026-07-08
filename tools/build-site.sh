#!/usr/bin/env bash
# Assemble the GitHub Pages site into ./site/:
#
#   /            README.md rendered to HTML (GitHub's own renderer)
#   /galaksija/  the web emulator (requires tools/build-wasm.sh first)
#   /assets/     logo + screenshots referenced by the README
#
# Used by .github/workflows/pages.yml; runs locally too (set GITHUB_TOKEN
# to avoid the anonymous rate limit of the markdown API).
set -euo pipefail
cd "$(dirname "$0")/.."

REPO_URL="https://github.com/mtrisic/vremeplov"
OUT=site

test -f web/vremeplov.wasm || {
    echo "error: emulator not built — run tools/build-wasm.sh first" >&2
    exit 1
}

rm -rf "$OUT"
mkdir -p "$OUT/galaksija"
cp web/index.html web/vremeplov.wasm web/wasm_exec.js "$OUT/galaksija/"
cp assets/logo/icon-256.png "$OUT/galaksija/"
cp -r assets "$OUT/assets"
find "$OUT" -name .DS_Store -delete
cp assets/logo/favicon-16.png assets/logo/favicon-32.png "$OUT/"
cp assets/logo/favicon-16.png assets/logo/favicon-32.png "$OUT/galaksija/"

# Repo-file links in the README have no target on the Pages site;
# point them at GitHub before rendering.
sed -E "s#\]\((SPEC\.md|PLAN\.md|AGENTS\.md|LICENSE|roms/PROVENANCE\.md|examples/[^)]+)\)#]($REPO_URL/blob/main/\1)#g" \
    README.md > "$OUT/.readme.md"

auth=()
if [ -n "${GITHUB_TOKEN:-}" ]; then
    auth=(-H "Authorization: Bearer $GITHUB_TOKEN")
fi
curl -fsS "${auth[@]}" -H "Content-Type: text/plain" \
    --data-binary @"$OUT/.readme.md" \
    https://api.github.com/markdown/raw > "$OUT/.readme.html"

cat > "$OUT/index.html" <<'HEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Vremeplov — cycle-accurate Galaksija emulator in Go</title>
<link rel="icon" type="image/png" sizes="32x32" href="favicon-32.png">
<link rel="icon" type="image/png" sizes="16x16" href="favicon-16.png">
<link rel="stylesheet"
      href="https://cdnjs.cloudflare.com/ajax/libs/github-markdown-css/5.8.1/github-markdown.min.css">
<style>
  body { margin: 0; }
  .markdown-body { box-sizing: border-box; min-width: 200px; max-width: 980px;
                   margin: 0 auto; padding: 45px; }
  @media (max-width: 767px) { .markdown-body { padding: 15px; } }
</style>
</head>
<body>
<article class="markdown-body">
HEAD
cat "$OUT/.readme.html" >> "$OUT/index.html"
printf '</article>\n</body>\n</html>\n' >> "$OUT/index.html"
rm "$OUT/.readme.md" "$OUT/.readme.html"

echo "site/ assembled: $(find "$OUT" -type f | wc -l | tr -d ' ') files"
