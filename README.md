# cachehdr

A small Go CLI and library that audits the `Cache-Control` headers a static site sends. It fetches one HTML page, finds the same-origin scripts, stylesheets, fonts and images it references, and checks every response against a simple policy. Standard library only, no config file.

## Why

When you move a site from Netlify or Vercel to your own Nginx or Caddy, the caching rules those platforms set for you disappear. Two mistakes are common after the move:

- HTML gets a long `max-age`, so returning visitors keep an old page that points at asset files the new deploy deleted. The result is a blank page or missing styles until they hard refresh.
- Hashed bundles like `index.3f9a1c2b.js` get no caching at all, so every page view downloads them again even though their content can never change.

cachehdr shows both in one run.

## Install

```
go install github.com/PrinceAdvant/cachehdr/cmd/cachehdr@latest
```

## Usage

```
cachehdr https://example.com/
cachehdr -max 50 -json https://example.com/docs/
```

Example output:

```
FIX  html          200 https://example.com/
     Cache-Control: public, max-age=86400
     hint: HTML is cached for more than 5 minutes; visitors can keep old pages that point to deleted assets
ok   fingerprinted 200 https://example.com/assets/index.3f9a1c2b.js
     Cache-Control: public, max-age=31536000, immutable
FIX  plain-asset   200 https://example.com/css/site.css
     Cache-Control: max-age=31536000
     hint: file name has no hash but is cached for more than a day; a deploy will not reach returning visitors
3 checked, 2 to fix
```

The exit code is 1 when anything needs fixing, so it works as a post-deploy check in CI.

## The policy

- **HTML**: `no-cache` or `max-age=0` passes. Up to 5 minutes is tolerated. A page with only `s-maxage` passes with a reminder to purge the CDN on deploy, because browsers ignore `s-maxage`.
- **Fingerprinted assets** (a hash in the file name, or under `/_next/static/`): should be `public, max-age=31536000, immutable`.
- **Plain assets**: anything over a day is flagged, because the next deploy cannot replace them in browsers that already have a copy.

A matching Nginx snippet:

```
location / { add_header Cache-Control "no-cache"; try_files $uri $uri/ /index.html; }
location /assets/ { add_header Cache-Control "public, max-age=31536000, immutable"; }
```

For the bigger picture of serving a site yourself, see this guide to [deploy a static site on your own server](https://peon.sh/blogs/deploy-static-site-own-server). Built by a contributor to [Peon](https://peon.sh/), an open-source self-hosted deployment platform.

MIT licensed.
