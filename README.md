# Avian

Avian is a birdwatching community application for CS 408 built with Go, Echo, SQLite, and server-rendered HTML. Checkpoint 4 adds the species field guide with credited reference photos, a paginated Feed, and chirp creation, editing, confirmed deletion, species tags, and protected observation photos.

The current implementation report is in [docs/checkpoint-4.md](docs/checkpoint-4.md); earlier work is recorded in [docs/checkpoint-3.md](docs/checkpoint-3.md) and [docs/checkpoint-2.md](docs/checkpoint-2.md). Flock management and observation search retain shared page shells for Checkpoints 5 and 6. Flock memberships supplied by test fixtures exercise flock-only posts and photos until the flock UI arrives.

## Run from a fresh clone

On Ubuntu LTS or Ubuntu under WSL, run this from the repository root:

```bash
./start.sh
```

Open <http://localhost:8080/>. Stop the foreground server with Ctrl+C. Repeat the command to restart; SQLite data and local configuration are retained. The implementation uses Go 1.22.2 and was checked on Ubuntu 24.04.4 LTS.

`start.sh` uses system Go 1.22 or newer when available; otherwise it downloads Go 1.22.2 into the ignored `.tools/` directory. On Ubuntu it installs `curl` and `tar` using `apt-get` if needed. It downloads modules, generates private local configuration, builds Avian, and starts it. Startup applies pending migrations and inserts missing species. A fresh clone requires network access for dependencies. Bootstrap and illustration assets are committed; no Node toolchain, CDN, or database server is required.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port when `LISTEN_ADDR` is unset. |
| `LISTEN_ADDR` | `:<PORT>` | EC2 uses `127.0.0.1:8080` behind Nginx. |
| `DATABASE_PATH` | `./data/avian.db` | Persistent SQLite database. |
| `UPLOAD_DIR` | `./data/uploads` | Private persistent storage for uploaded observation photos, outside `static/`. |
| `SESSION_SECRET` | Random value in `.env` | At least 32 characters; signs anonymous CSRF tokens. |
| `COOKIE_SECURE` | `false` | Set to `true` when the site is served over HTTPS. The course EC2 site uses HTTP. |

Set variables in your shell or local `.env`. The startup script creates `.env` with mode 600 and a random secret on first launch. The production executable requires a secret of at least 32 characters. `.env`, `data/`, and `.tools/` are ignored by Git. Keep SSH keys outside the repository.

## Routes and accounts

All pages share a header, footer, local Bootstrap styles, the field-journal palette, and links to Home, Feed, Species, Flocks, Search, My Profile, Write a Chirp, and Create a Flock. Signed-out account actions return visitors to their requested destination after authentication. Home uses original bird artwork, seeded species previews, and up to three recent public chirps read from SQLite. It never previews flock-only chirps; a new database shows an empty community state.

`GET /login` and `GET /login?tab=register` switch forms without JavaScript. Registration validates a unique email and username, password length and confirmation, normalizes identities to lowercase, and atomically creates the credentials and public member record. Form errors retain email, username, selected tab, and return destination; passwords are not reflected. Passwords use bcrypt at its default cost. Login returns a generic failure for invalid credentials. Successful registration signs the new member in.

Authentication uses a cryptographically random token in an HttpOnly, SameSite=Lax cookie with a seven-day expiration. SQLite stores only its SHA-256 hash, a separate CSRF token, and expiration; sessions persist through server restarts. Anonymous CSRF tokens are signed with HMAC-SHA256. Every state-changing route, including login and registration, requires its form token. Login rotates the session and CSRF token. `POST /logout` deletes the session and clears the cookies; `GET /logout` does not log anyone out. Expired sessions are rejected. Return destinations are restricted to local paths. Authenticated pages use `Cache-Control: no-store`.

`GET /members/:id` displays public identity, bio, broad region, the ten newest public chirps, and linked flock memberships. Emails, credentials, and flock-only chirps never appear in profiles, even for the profile owner. Empty profiles have explicit empty states. Only the signed-in owner sees Edit Profile and account deletion controls.

`GET /members/:id?edit=1` opens a prefilled inline form on the same page. `POST /members/:id` validates and saves username, display name, region, and bio. Usernames use the registration rules and case-insensitive uniqueness; display names require 1–80 characters, optional regions allow 100, and optional bios allow 500. Text limits count Unicode characters. Failed validation retains the form and entered values; success redirects to the profile. Public text is escaped by Go templates. Email, credentials, member IDs, and account ownership cannot be edited through this form.

`POST /members/:id/delete` requires the owner's authenticated session, CSRF token, and an explicit permanent-deletion confirmation. A transaction blocks any current flock owner, repairs original-creator references after a completed ownership transfer, and deletes the account with its profile, all sessions, chirps, tags, and memberships through foreign-key cascades. Chirp photos are cleaned up after commit, restricted to filenames inside `UPLOAD_DIR`; failures are logged. Both session and CSRF cookies are cleared. Flock ownership-transfer controls arrive in Checkpoint 5; fixture memberships exercise the deletion restrictions now. Profile writes reject guests with HTTP 401, other members with 403, and invalid or missing member IDs with 404. Unconfirmed deletion returns 422; ownership conflicts return 409. `GET /api/health` checks the database and reports the deployed source commit in its `X-Avian-Revision` header.

## Database

`main.go` initializes the store before serving HTTP. SQLite foreign keys and a busy timeout are enabled. Embedded migrations apply in filename order; each migration and its ledger record commit together. Species seeds insert missing common names without duplicating or overwriting records.

The eight core tables are `users`, `members`, `sessions`, `species`, `flocks`, `flock_members`, `chirps`, and `chirp_species`. Their constraints and indexes are in `migrations/001_initial.sql`. Migration 002 adds case-insensitive email and username uniqueness. Migration 003 adds species photo credits. The catalog has 12 project-written summaries with Cornell Lab All About Birds reference links and locally bundled Wikimedia photos. Startup backfills reference photos into existing catalogs without changing species IDs or descriptions.

## Species, Feed, chirps, and photos

`GET /species` provides a searchable common/scientific-name catalog, quick-access sidebar, and photo cards. `?selected=<id>` opens reference information, habitat, source and license links, and tagged community chirps in the same page. `q` retains the catalog search; pagination retains both `q` and the selected species. Missing or invalid species return 404.

`GET /feed?page=<n>` shows 20 visible chirps per page, ordered by creation time and ID descending. Visitors see public posts; signed-in members additionally see flock-only posts from their current flocks. Species results use the same visibility predicate and pagination. Cards link authors, tagged species, and flock destinations. Only authors get edit and confirmed-deletion controls. Home and public profiles continue to show only public chirps.

`GET /chirps/form` opens a signed-in author's editor; `?id=<id>` prefills an owned post. `POST /chirps/form` creates or updates it with 1–500 Unicode characters, zero or more existing species, optional flock association, public/flock-only visibility, and one optional photo. A flock destination requires current membership, including for associated public posts; flock-only visibility requires a destination. Saving the post and replacing its tags use one transaction. Validation errors retain entered fields and choices. Browsers require a photo to be selected again after an error. Successful writes redirect to Feed.

`POST /chirps/:id/delete` requires the author, a valid CSRF token, and `confirm_delete=yes`. It removes the post and tags transactionally, then removes its photo. Guests cannot write, other accounts cannot edit or delete, and GET never deletes. Image replacement/removal also cleans up the old file only after commit; failed saves remove newly written images.

Uploads require confirmation that the member owns or may share the photo. The server accepts one actual JPEG or PNG, at most 5 MiB and 3000 × 3000 pixels, decodes it, and re-encodes pixels to discard metadata, including GPS data. It ignores the client filename and declared MIME type, uses a random stored filename with mode 600, and limits the entire chirp form to 6 MiB. Multipart forms retain session-specific CSRF checks. Larger requests return 413; invalid photos and fields return 422.

`GET /chirps/:id/photo` checks the post's current visibility on every request. Private photos return 404 to guests and nonmembers; leaving a flock or changing visibility takes effect immediately. Responses use `nosniff` and `private, no-store`. Missing files, unsafe stored paths, and symlinks return 404. Uploaded files have no static route. Keep `UPLOAD_DIR` outside the web root and preserve it alongside the database during deployment.

## EC2 setup and deployment

The EC2 instance is <http://ec2-52-89-244-87.us-west-2.compute.amazonaws.com/>. Allow public HTTP on port 80 and restrict SSH to your address. Port 8080 stays private. The local deployment machine needs Git, Go (or the toolchain from `start.sh`), SSH, SCP, rsync, and curl.

For initial setup, copy `deploy/` to your Ubuntu LTS server and run there:

```bash
sudo bash deploy/setup-ec2.sh
```

Setup creates the unprivileged `avian` account, root-owned `/opt/avian` release directory, private `/var/lib/avian` database/uploads, and mode-600 `/etc/avian/avian.env`. Nginx forwards port 80 to `127.0.0.1:8080`. Both services are enabled at boot. The systemd service prevents privilege escalation, uses a private temporary directory, and permits filesystem writes only under `/var/lib/avian`. Repeating setup preserves the environment file and data.

When upgrading an existing Checkpoint 3 server, copy the updated `deploy/` directory and rerun `sudo bash deploy/setup-ec2.sh` before deploying Checkpoint 4. It raises Nginx's request limit to 6 MiB so the proxy accepts the application's 5 MiB photos plus multipart fields.

Commit and tag the tested release, then deploy from that exact commit:

```bash
EC2_HOST=ec2-52-89-244-87.us-west-2.compute.amazonaws.com \
SSH_KEY_PATH=/path/to/key.pem DEPLOY_TAG=cp4 bash deploy/deploy.sh
```

`EC2_USER` defaults to `ubuntu`; `DEPLOY_TAG` defaults to `cp4` and can name a later checkpoint. Deployment rejects uncommitted changes and a HEAD that differs from the requested tag. It detects the instance architecture, cross-compiles the Go binary with the source revision, transfers the binary and static assets, atomically replaces the executable, restarts the service, and verifies the revision over public HTTP. Templates and migrations are embedded. Database, uploads, and server configuration are retained.

Useful server checks:

```bash
sudo systemctl status avian nginx
sudo systemctl is-enabled avian nginx
sudo systemctl show avian -p User -p Group -p UMask -p ProtectSystem -p ReadWritePaths
sudo journalctl -u avian -n 100 --no-pager
curl -i http://localhost/api/health
```

To demonstrate boot startup, note the instance boot ID, run `sudo reboot` over SSH, reconnect, and confirm a changed boot ID, active/enabled services, and the public health revision without manually starting the app. Verify a previously created account can still log in. Wait for SSH and HTTP to recover before continuing.

## Verification

```bash
go test ./...
go test -race ./...
go vet ./...
bash -n start.sh deploy/setup-ec2.sh deploy/deploy.sh
```

The tests use temporary SQLite databases and real HTTP handlers. They cover migrations and repeatable seeds; registration validation and transactional duplicate rejection; bcrypt and session-hash storage; successful/failed login; session restoration, expiration, and revocation; CSRF failures and rotation; safe redirect destinations; shared navigation; missing members; and Home's newest-three public-only previews and HTML escaping. Secure-cookie configuration is tested separately.

The profile tests additionally cover persisted inline edits, Unicode field limits, duplicate usernames, escaped content, public-only chirps, membership links, guest and cross-account write rejection, explicit deletion confirmation, all-session revocation, dependent-row and photo cleanup, owner rejection, transferred creator references, and transactional rollback. Database reopen checks verify saved edits and sessions survive a restart.

`chirps_test.go` and `photos_test.go` cover create/edit/delete, Unicode limits, tag replacement and duplicate handling, transaction rollback, field retention, author permissions, CSRF, stable pagination, species selection/search, complete bundled photo credits, repeatable catalog upgrades, Feed/Species/photo membership filtering, and persisted posts/tags/photos/sessions after a database reopen. Upload cases include invalid/truncated formats, byte/dimension/request limits, multiple files, ownership confirmation, client filename rejection, metadata stripping, replacement/removal cleanup, database-failure cleanup, private HTTP headers, missing files, unsafe paths, and symlinks.

For a clean startup check, clone the submitted tag into a new directory and run `./start.sh` there without copying `.env`, `.tools`, or `data`. Check Home, catalog photos, registration, a tagged photo chirp, Feed, editing, confirmed deletion, logout, and login. Checkpoint 4 deployment and submission steps are recorded separately from local implementation verification in the current report.

## Media credits

- `static/images/field-notes.svg` and `static/images/mark.svg`: original Avian vector artwork created for this project with Codex assistance.
- Bootstrap 5.3.3: bundled official CSS, copyright The Bootstrap Authors, MIT license included at `static/vendor/bootstrap/LICENSE`.
- Species summaries: project-written text. Cornell Lab All About Birds links provide further reading; no Cornell images or article text are reproduced.
- Species reference photos: Wikimedia Commons files with per-file credits below and machine-readable records in `static/images/species/credits.json`. Source-file author and license metadata were checked through the Commons API on November 3, 2026. Bundled copies are Wikimedia thumbnails resized to 960 pixels; card display may crop them. Each image retains its listed license, including ShareAlike where applicable. Attribution and source/license links appear beside the photos in the app.

| Species | Photographer / credit | Wikimedia source | License |
| --- | --- | --- | --- |
| American Goldfinch | Rodney Campbell | [Original file](https://commons.wikimedia.org/wiki/File:Carduelis_tristis_-Michigan,_USA_-male-8.jpg) | [CC BY 2.0](https://creativecommons.org/licenses/by/2.0/) |
| American Robin | PookieFugglestein | [Original file](https://commons.wikimedia.org/wiki/File:American_Robin_3.JPG) | [CC0](https://creativecommons.org/publicdomain/zero/1.0/) |
| Black-capped Chickadee | Mdf (attributed by Commons) | [Original file](https://commons.wikimedia.org/wiki/File:Poecile-atricapilla-001.jpg) | [CC BY-SA 3.0](https://creativecommons.org/licenses/by-sa/3.0/) |
| Canada Goose | USFWS Mountain-Prairie | [Original file](https://commons.wikimedia.org/wiki/File:Canada_goose_on_Seedskadee_NWR_(27826185489).jpg) | [Public domain](https://commons.wikimedia.org/wiki/Commons:Public_domain) |
| Dark-eyed Junco | Cephas | [Original file](https://commons.wikimedia.org/wiki/File:Junco_hyemalis_hyemalis_CT1_(cropped).jpg) | [CC BY-SA 3.0](https://creativecommons.org/licenses/by-sa/3.0/) |
| European Starling | Charles J. Sharp | [Original file](https://commons.wikimedia.org/wiki/File:Common_starling_(Sturnus_vulgaris)_breeding_male_Marken.jpg) | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) |
| House Finch | Rhododendrites | [Original file](https://commons.wikimedia.org/wiki/File:House_finch_(33688)2.jpg) | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) |
| Mallard | Richard Bartz | [Original file](https://commons.wikimedia.org/wiki/File:Anas_platyrhynchos_male_female_quadrat.jpg) | [CC BY-SA 2.5](https://creativecommons.org/licenses/by-sa/2.5/) |
| Mourning Dove | Rhododendrites | [Original file](https://commons.wikimedia.org/wiki/File:Mourning_dove_perched_(61179).jpg) | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) |
| Northern Flicker | Rhododendrites | [Original file](https://commons.wikimedia.org/wiki/File:Northern_yellow-shafted_flicker_male_(33737)_(cropped).jpg) | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) |
| Red-tailed Hawk | Becky Matsubara | [Original file](https://commons.wikimedia.org/wiki/File:Red-tailed_Hawk_(45812546121).jpg) | [CC BY 2.0](https://creativecommons.org/licenses/by/2.0/) |
| Song Sparrow | Rhododendrites | [Original file](https://commons.wikimedia.org/wiki/File:Song_sparrow_in_Prospect_Park_(93031).jpg) | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) |
