# The Mogi landing page — deploying & the custom domain

The marketing site at **[trymogi.dev](https://trymogi.dev)**. Plain HTML + CSS, no
build step, no framework, no dependencies — `index.html` and `style.css` are
served exactly as they sit in this folder.

| File | What it is |
|---|---|
| `index.html` | The whole page, including the two inline scripts (latest-release lookup, section nav) |
| `style.css` | Design tokens copied from `frontend/src/style.css` so the site matches the app |
| `appicon.png` | Favicon + wordmark, copied from `build/appicon.png` |
| `CNAME` | The custom domain. **Must stay in this folder** — see [Why CNAME lives here](#why-cname-lives-in-this-folder) |

## How deployment works

[`.github/workflows/pages.yml`](../.github/workflows/pages.yml) uploads this
folder to GitHub Pages. It runs when you push to `main` **and** the push touched
`site/**` — so app work never republishes the site, and site work never spends a
macOS build (`build.yml` has a matching `paths-ignore`).

There is no build step. What you see in this folder is what ships.

---

## One-time setup

Do these **in order**. DNS first, because propagation is the slow part and
GitHub can't issue an HTTPS certificate until the domain resolves to it.

### 1. Point DNS at GitHub

At whichever registrar holds `trymogi.dev`, create these records.

**Apex (`trymogi.dev`) — four A records:**

```
A   @   185.199.108.153
A   @   185.199.109.153
A   @   185.199.110.153
A   @   185.199.111.153
```

**Apex — four AAAA records (IPv6; optional but recommended):**

```
AAAA   @   2606:50c0:8000::153
AAAA   @   2606:50c0:8001::153
AAAA   @   2606:50c0:8002::153
AAAA   @   2606:50c0:8003::153
```

**`www` subdomain — one CNAME**, so `www.trymogi.dev` redirects to the apex:

```
CNAME   www   zyang310.github.io.
```

Notes:

- Some registrars support **ALIAS / ANAME / CNAME flattening** at the apex. If
  yours does, a single `ALIAS @ → zyang310.github.io` replaces all eight records
  above and keeps working if GitHub ever changes its IPs.
- **On Cloudflare, turn the proxy OFF** (grey cloud, "DNS only") for these
  records. With the orange cloud on, GitHub can't verify the domain or issue its
  certificate, and you get a redirect loop.

### 2. Push the site

```bash
git add site .github/workflows/pages.yml
git commit -m "chore: add landing page"
git push
```

Use `chore:` or `docs:`, **not** `feat:` — the site isn't part of the app
binary, and `feat:` would make release-please cut a new app version and tell
every user to re-download for a website change.

### 3. Turn on Pages with the Actions source

**Settings → Pages → Build and deployment → Source: `GitHub Actions`**

This is the step people get wrong. The default, *Deploy from a branch*, can only
serve the repo root or `/docs` — it cannot see `site/`, so leaving it selected
means the workflow deploys and the site still 404s.

### 4. Wait for the certificate, then force HTTPS

Once DNS resolves, GitHub automatically requests a Let's Encrypt certificate.
This usually takes a few minutes and occasionally up to an hour. When
**Settings → Pages → Enforce HTTPS** stops being greyed out, tick it.

`.dev` is on the [HSTS preload list](https://hstspreload.org), so browsers refuse
plain HTTP for it entirely. Until the certificate is issued, the site is not
merely insecure — it does not load at all. This is normal; wait it out.

### 5. Optional: verify domain ownership

**Settings → Pages → Verify domain** gives you a TXT record to add. It stops
anyone else from ever attaching your domain to their GitHub Pages site if your
DNS were to lapse. Two minutes, worth doing.

---

## Verifying it worked

```bash
# DNS points at GitHub's four addresses
dig +short trymogi.dev

# The site is up and serving over HTTPS
curl -sI https://trymogi.dev | head -1        # expect: HTTP/2 200

# The certificate covers the domain
curl -sI https://trymogi.dev | grep -i strict-transport

# www redirects to the apex
curl -sI https://www.trymogi.dev | head -2
```

The deploy job also prints the live URL in its summary
(**Actions → Pages → the run → deploy**).

---

## Day to day

Edit `index.html` or `style.css`, commit, push. The workflow redeploys in about
a minute. Nothing else to do.

To preview locally before pushing:

```bash
cd site && python3 -m http.server 4321
# then open http://localhost:4321
```

To redeploy without changing anything (e.g. after fixing DNS):
**Actions → Pages → Run workflow**.

---

## Why CNAME lives in this folder

With branch-based Pages, GitHub writes a `CNAME` file into the published branch
itself, and it is easy to lose — any deploy that overwrites the branch drops it,
and the custom domain silently detaches.

Deploying from an artifact means whatever is in `site/` *is* the published site,
so `site/CNAME` is version-controlled, reviewable, and can't be clobbered by a
deploy. Delete it and the next deploy unsets the custom domain.

## Why the site isn't in `/docs`

GitHub's branch-based Pages only offers the repo root or `/docs` as a source, and
`/docs` already holds this project's written documentation (architecture,
roadmap, plans). Rather than mix a marketing page into that folder or move the
docs, the site lives in `site/` and ships via the Actions deploy, which can
publish any folder.

---

## Troubleshooting

| Symptom | Cause |
|---|---|
| 404 at the custom domain | Source is still *Deploy from a branch*. Switch to **GitHub Actions** (step 3). |
| Site loads at `zyang310.github.io` but not `trymogi.dev` | DNS hasn't propagated, or the A records are wrong. Check with `dig +short trymogi.dev`. |
| "Enforce HTTPS" is greyed out | The certificate hasn't been issued yet — DNS must resolve first. Wait, then reload the page. |
| Browser refuses to connect at all | Expected before the certificate exists, because `.dev` is HSTS-preloaded. Not a misconfiguration. |
| Redirect loop | Cloudflare proxy is on. Set those records to **DNS only** (grey cloud). |
| Custom domain keeps unsetting itself | `site/CNAME` was deleted or its contents changed. |
| Download button shows no version | The GitHub API call failed; the button falls back to the releases page and still works. |
| Pushed a change, nothing deployed | The push didn't touch `site/**`. Trigger it manually via **Run workflow**. |
