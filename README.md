# Avian

Avian is a birdwatching community application for CS 408 built with Go, Echo, SQLite, and server-rendered HTML. Checkpoint 2 adds the shared site design, finished Home page, registration, login/logout, persisted sessions, CSRF protection, and a verified EC2 release.

The Checkpoint 2 scope and evidence are in [docs/checkpoint-2.md](docs/checkpoint-2.md). Feed, Species detail/photos, profile editing, chirp management, flocks, and observation search follow the approved schedule; their navigation destinations currently share the site layout and explain their availability. The existing species catalog and basic member identity display are available now.

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
| `UPLOAD_DIR` | `./data/uploads` | Persistent directory reserved for later uploads. |
| `SESSION_SECRET` | Random value in `.env` | At least 32 characters; signs anonymous CSRF tokens. |
| `COOKIE_SECURE` | `false` | Set to `true` when the site is served over HTTPS. The course EC2 site uses HTTP. |

Set variables in your shell or local `.env`. The startup script creates `.env` with mode 600 and a random secret on first launch. The production executable requires a secret of at least 32 characters. `.env`, `data/`, and `.tools/` are ignored by Git. Keep SSH keys outside the repository.

## Routes and accounts

All pages share a header, footer, local Bootstrap styles, the field-journal palette, and links to Home, Feed, Species, Flocks, Search, My Profile, Write a Chirp, and Create a Flock. Signed-out account actions return visitors to their requested destination after authentication. Home uses original bird artwork, seeded species previews, and up to three recent public chirps read from SQLite. It never previews flock-only chirps; a new database shows an empty community state.

`GET /login` and `GET /login?tab=register` switch forms without JavaScript. Registration validates a unique email and username, password length and confirmation, normalizes identities to lowercase, and atomically creates the credentials and public member record. Form errors retain email, username, selected tab, and return destination; passwords are not reflected. Passwords use bcrypt at its default cost. Login returns a generic failure for invalid credentials. Successful registration signs the new member in.

Authentication uses a cryptographically random token in an HttpOnly, SameSite=Lax cookie with a seven-day expiration. SQLite stores only its SHA-256 hash, a separate CSRF token, and expiration; sessions persist through server restarts. Anonymous CSRF tokens are signed with HMAC-SHA256. Every state-changing route, including login and registration, requires its form token. Login rotates the session and CSRF token. `POST /logout` deletes the session and clears the cookies; `GET /logout` does not log anyone out. Expired sessions are rejected. Return destinations are restricted to local paths. Authenticated pages use `Cache-Control: no-store`.

`GET /members/:id` displays public identity only; emails and password hashes never appear in profiles. Profile editing and account deletion belong to Checkpoint 3. `GET /api/health` checks the database and reports the deployed source commit in its `X-Avian-Revision` header.

## Database

`main.go` initializes the store before serving HTTP. SQLite foreign keys and a busy timeout are enabled. Embedded migrations apply in filename order; each migration and its ledger record commit together. Species seeds insert missing common names without duplicating or overwriting records.

The eight core tables are `users`, `members`, `sessions`, `species`, `flocks`, `flock_members`, `chirps`, and `chirp_species`. Their constraints and indexes are in `migrations/001_initial.sql`. Migration 002 adds case-insensitive email and username uniqueness. The catalog has 12 project-written summaries with Cornell Lab All About Birds reference links. Reference photos and license records arrive in Checkpoint 4.

## EC2 setup and deployment

The Checkpoint 2 instance is <http://ec2-52-89-244-87.us-west-2.compute.amazonaws.com/>. Allow public HTTP on port 80 and restrict SSH to your address. Port 8080 stays private. The local deployment machine needs Git, Go (or the toolchain from `start.sh`), SSH, SCP, rsync, and curl.

For initial setup, copy `deploy/` to your Ubuntu LTS server and run there:

```bash
sudo bash deploy/setup-ec2.sh
```

Setup creates the unprivileged `avian` account, root-owned `/opt/avian` release directory, private `/var/lib/avian` database/uploads, and mode-600 `/etc/avian/avian.env`. Nginx forwards port 80 to `127.0.0.1:8080`. Both services are enabled at boot. The systemd service prevents privilege escalation, uses a private temporary directory, and permits filesystem writes only under `/var/lib/avian`. Repeating setup preserves the environment file and data.

Commit and tag the tested release, then deploy from that exact commit:

```bash
EC2_HOST=ec2-52-89-244-87.us-west-2.compute.amazonaws.com \
SSH_KEY_PATH=/path/to/key.pem DEPLOY_TAG=cp2 bash deploy/deploy.sh
```

`EC2_USER` defaults to `ubuntu`; `DEPLOY_TAG` defaults to `cp2` and can name a later checkpoint. Deployment rejects uncommitted changes and a HEAD that differs from the requested tag. It detects the instance architecture, cross-compiles the Go binary with the source revision, transfers the binary and static assets, atomically replaces the executable, restarts the service, and verifies the revision over public HTTP. Templates and migrations are embedded. Database, uploads, and server configuration are retained.

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
go vet ./...
bash -n start.sh deploy/setup-ec2.sh deploy/deploy.sh
```

The tests use temporary SQLite databases and real HTTP handlers. They cover migrations and repeatable seeds; registration validation and transactional duplicate rejection; bcrypt and session-hash storage; successful/failed login; session restoration, expiration, and revocation; CSRF failures and rotation; safe redirect destinations; shared navigation; missing members; and Home's newest-three public-only previews and HTML escaping. Secure-cookie configuration is tested separately.

For a clean startup check, clone the submitted tag into a new directory and run `./start.sh` there without copying `.env`, `.tools`, or `data`. Check Home, local Bootstrap and artwork assets, registration, logout, and login. The Checkpoint 2 release notes record the deployment and fresh-clone checks.

## Media credits

- `static/images/field-notes.svg` and `static/images/mark.svg`: original Avian vector artwork created for this project with Codex assistance. No external photo assets are used in Checkpoint 2.
- Bootstrap 5.3.3: bundled official CSS, copyright The Bootstrap Authors, MIT license included at `static/vendor/bootstrap/LICENSE`.
- Species summaries: project-written text. Cornell Lab All About Birds links provide further reading; no Cornell images or article text are reproduced.
